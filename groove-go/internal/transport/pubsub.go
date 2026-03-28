package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/safecast/groove-go/internal/store"
)

// Message is the wire format for all workspace messages.
type Message struct {
	From      string    `json:"from"`
	Workspace string    `json:"workspace"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"ts"`
}

// Workspace represents a joined pubsub topic (a shared channel).
type Workspace struct {
	name  string
	self  peer.ID
	topic *pubsub.Topic
	sub   *pubsub.Subscription
	ps    *pubsub.PubSub
	store *store.Store
}

// NewGossipSub creates a GossipSub router attached to the given host.
func NewGossipSub(ctx context.Context, h host.Host) (*pubsub.PubSub, error) {
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("gossipsub: %w", err)
	}
	fmt.Println("[transport] GossipSub router started")
	return ps, nil
}

// JoinWorkspace joins (or creates) a pubsub topic for the named workspace.
// Pass a non-nil store to enable persistence.
func JoinWorkspace(ps *pubsub.PubSub, self peer.ID, name string, s *store.Store) (*Workspace, error) {
	topic, err := ps.Join("workspace-" + name)
	if err != nil {
		return nil, fmt.Errorf("join topic: %w", err)
	}

	sub, err := topic.Subscribe()
	if err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}

	fmt.Printf("[transport] joined workspace: %s\n", name)
	return &Workspace{
		name:  name,
		self:  self,
		topic: topic,
		sub:   sub,
		ps:    ps,
		store: s,
	}, nil
}

// Publish sends a message to the workspace topic and persists it locally.
func (w *Workspace) Publish(ctx context.Context, body string) error {
	msg := Message{
		From:      w.self.String(),
		Workspace: w.name,
		Body:      body,
		Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if err := w.topic.Publish(ctx, data); err != nil {
		return err
	}
	if w.store != nil {
		_ = w.store.Save(store.Message(msg))
	}
	return nil
}

// ReadLoop blocks and prints every incoming message, persisting each one.
// Returns when ctx is cancelled.
func (w *Workspace) ReadLoop(ctx context.Context) {
	for {
		m, err := w.sub.Next(ctx)
		if err != nil {
			return
		}
		// Skip our own messages — already echoed and saved on Publish.
		if m.ReceivedFrom == w.self {
			continue
		}
		var msg Message
		if err := json.Unmarshal(m.Data, &msg); err != nil {
			continue
		}
		fmt.Printf("[%s] %s: %s\n", msg.Workspace, shortID(msg.From), msg.Body)
		if w.store != nil {
			_ = w.store.Save(store.Message(msg))
		}
	}
}

// ListPeers returns the peer IDs currently subscribed to this workspace topic.
func (w *Workspace) ListPeers() []peer.ID {
	return w.ps.ListPeers("workspace-" + w.name)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
