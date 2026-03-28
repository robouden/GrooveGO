package presence

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	heartbeatInterval = 5 * time.Second
	offlineThreshold  = 15 * time.Second // missed 3 heartbeats = offline
)

// beat is the heartbeat wire format.
type beat struct {
	PeerID    string    `json:"peer_id"`
	Workspace string    `json:"workspace"`
	Timestamp time.Time `json:"ts"`
}

// PeerInfo holds the last-seen time for a peer.
type PeerInfo struct {
	ID       peer.ID
	LastSeen time.Time
}

// Tracker publishes heartbeats and tracks which peers are online.
type Tracker struct {
	self      peer.ID
	workspace string
	topic     *pubsub.Topic
	sub       *pubsub.Subscription

	mu    sync.RWMutex
	peers map[peer.ID]time.Time
}

// New creates and starts a Tracker for the given workspace pubsub instance.
// It joins a dedicated presence topic (separate from the chat topic).
func New(ctx context.Context, ps *pubsub.PubSub, self peer.ID, workspace string) (*Tracker, error) {
	topicName := "presence-" + workspace
	topic, err := ps.Join(topicName)
	if err != nil {
		return nil, fmt.Errorf("presence join topic: %w", err)
	}
	sub, err := topic.Subscribe()
	if err != nil {
		return nil, fmt.Errorf("presence subscribe: %w", err)
	}

	t := &Tracker{
		self:      self,
		workspace: workspace,
		topic:     topic,
		sub:       sub,
		peers:     make(map[peer.ID]time.Time),
	}

	go t.publishLoop(ctx)
	go t.receiveLoop(ctx)
	go t.reapLoop(ctx)

	fmt.Printf("[presence] tracking started for workspace: %s\n", workspace)
	return t, nil
}

// Online returns peer IDs seen within the offline threshold, excluding self.
func (t *Tracker) Online() []PeerInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	cutoff := time.Now().Add(-offlineThreshold)
	out := make([]PeerInfo, 0, len(t.peers))
	for id, lastSeen := range t.peers {
		if lastSeen.After(cutoff) {
			out = append(out, PeerInfo{ID: id, LastSeen: lastSeen})
		}
	}
	return out
}

// publishLoop sends a heartbeat every heartbeatInterval.
func (t *Tracker) publishLoop(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	t.sendBeat(ctx) // send immediately on start
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.sendBeat(ctx)
		}
	}
}

func (t *Tracker) sendBeat(ctx context.Context) {
	data, err := json.Marshal(beat{
		PeerID:    t.self.String(),
		Workspace: t.workspace,
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		return
	}
	_ = t.topic.Publish(ctx, data)
}

// receiveLoop listens for heartbeats from other peers and updates the map.
func (t *Tracker) receiveLoop(ctx context.Context) {
	for {
		m, err := t.sub.Next(ctx)
		if err != nil {
			return
		}
		var b beat
		if err := json.Unmarshal(m.Data, &b); err != nil {
			continue
		}
		id, err := peer.Decode(b.PeerID)
		if err != nil || id == t.self {
			continue
		}
		t.mu.Lock()
		t.peers[id] = time.Now()
		t.mu.Unlock()
	}
}

// reapLoop removes peers that haven't been seen in a while.
func (t *Tracker) reapLoop(ctx context.Context) {
	ticker := time.NewTicker(offlineThreshold)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-offlineThreshold)
			t.mu.Lock()
			for id, lastSeen := range t.peers {
				if lastSeen.Before(cutoff) {
					delete(t.peers, id)
				}
			}
			t.mu.Unlock()
		}
	}
}
