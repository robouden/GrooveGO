package transport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/safecast/groove-go/internal/store"
)

const (
	MsgTypeChat = "chat"
	MsgTypeFile = "file"
	MaxFileSize = 10 * 1024 * 1024 // 10 MB
)

// Message is the wire format for all workspace messages (chat and file).
type Message struct {
	From      string    `json:"from"`
	Workspace string    `json:"workspace"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"ts"`
	// File fields (only set when MsgType == "file")
	MsgType  string `json:"msg_type,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	FileName string `json:"file_name,omitempty"`
	FileMime string `json:"file_mime,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	FileData string `json:"file_data,omitempty"` // base64-encoded bytes
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

// NewGossipSub creates a GossipSub router with a 16 MB max message size.
func NewGossipSub(ctx context.Context, h host.Host) (*pubsub.PubSub, error) {
	ps, err := pubsub.NewGossipSub(ctx, h,
		pubsub.WithMaxMessageSize(16*1024*1024),
	)
	if err != nil {
		return nil, fmt.Errorf("gossipsub: %w", err)
	}
	fmt.Println("[transport] GossipSub router started")
	return ps, nil
}

// JoinWorkspace joins (or creates) a pubsub topic for the named workspace.
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
	return &Workspace{name: name, self: self, topic: topic, sub: sub, ps: ps, store: s}, nil
}

// Publish sends a chat message to the workspace topic and persists it locally.
func (w *Workspace) Publish(ctx context.Context, body string) error {
	msg := Message{
		MsgType:   MsgTypeChat,
		From:      w.self.String(),
		Workspace: w.name,
		Body:      body,
		Timestamp: time.Now().UTC(),
	}
	return w.publish(ctx, msg)
}

// PublishFile sends a file to the workspace topic (max 10 MB).
func (w *Workspace) PublishFile(ctx context.Context, fileID, fileName, mime string, data []byte) error {
	if len(data) > MaxFileSize {
		return fmt.Errorf("file too large (%d bytes, max %d)", len(data), MaxFileSize)
	}
	msg := Message{
		MsgType:   MsgTypeFile,
		From:      w.self.String(),
		Workspace: w.name,
		Body:      fmt.Sprintf("📎 %s (%s)", fileName, humanSize(int64(len(data)))),
		Timestamp: time.Now().UTC(),
		FileID:    fileID,
		FileName:  fileName,
		FileMime:  mime,
		FileSize:  int64(len(data)),
		FileData:  base64.StdEncoding.EncodeToString(data),
	}
	return w.publish(ctx, msg)
}

func (w *Workspace) publish(ctx context.Context, msg Message) error {
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

// ReadLoop blocks and prints every incoming message.
func (w *Workspace) ReadLoop(ctx context.Context) {
	w.ReadLoopInto(ctx, func(msg Message) {
		fmt.Printf("[%s] %s: %s\n", msg.Workspace, shortID(msg.From), msg.Body)
	})
}

// ReadLoopInto blocks and calls fn for every incoming message from other peers.
func (w *Workspace) ReadLoopInto(ctx context.Context, fn func(Message)) {
	for {
		m, err := w.sub.Next(ctx)
		if err != nil {
			return
		}
		if m.ReceivedFrom == w.self {
			continue
		}
		var msg Message
		if err := json.Unmarshal(m.Data, &msg); err != nil {
			continue
		}
		if w.store != nil {
			// Save metadata only (strip binary payload before storing)
			meta := msg
			meta.FileData = ""
			_ = w.store.Save(store.Message(meta))
		}
		fn(msg)
	}
}

// ListPeers returns the peer IDs currently in this workspace topic.
func (w *Workspace) ListPeers() []peer.ID {
	return w.ps.ListPeers("workspace-" + w.name)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func humanSize(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/1024/1024)
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
