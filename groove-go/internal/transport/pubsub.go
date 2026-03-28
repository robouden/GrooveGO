package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
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
func JoinWorkspace(ps *pubsub.PubSub, self peer.ID, name string) (*Workspace, error) {
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
	}, nil
}

// Publish sends a message to the workspace topic.
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
	return w.topic.Publish(ctx, data)
}

// ReadLoop blocks and prints every message received on the workspace topic.
// It returns when ctx is cancelled or the subscription is closed.
func (w *Workspace) ReadLoop(ctx context.Context) {
	for {
		m, err := w.sub.Next(ctx)
		if err != nil {
			return
		}
		// Skip our own messages — we already echo them on send.
		if m.ReceivedFrom == w.self {
			continue
		}
		var msg Message
		if err := json.Unmarshal(m.Data, &msg); err != nil {
			continue
		}
		fmt.Printf("[%s] %s: %s\n", msg.Workspace, shortID(msg.From), msg.Body)
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
