package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/safecast/groove-go/internal/presence"
	"github.com/safecast/groove-go/internal/store"
	"github.com/safecast/groove-go/internal/transport"
)

// entry holds an active workspace topic, its local store, and presence tracker.
type entry struct {
	ws      *transport.Workspace
	store   *store.Store
	tracker *presence.Tracker
}

// Manager owns multiple named workspace channels. It is safe for concurrent use.
type Manager struct {
	ps       *pubsub.PubSub
	self     peer.ID
	storeDir string

	mu     sync.RWMutex
	active map[string]*entry
}

// New creates a Manager. storeDir is the base directory for per-channel stores.
func New(ps *pubsub.PubSub, self peer.ID, storeDir string) *Manager {
	return &Manager{
		ps:       ps,
		self:     self,
		storeDir: storeDir,
		active:   make(map[string]*entry),
	}
}

// Join creates or returns an existing channel by name.
func (m *Manager) Join(ctx context.Context, name string) (*transport.Workspace, *store.Store, *presence.Tracker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.active[name]; ok {
		return e.ws, e.store, e.tracker, nil
	}

	s, err := store.Open(filepath.Join(m.storeDir, name))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open store for %q: %w", name, err)
	}

	ws, err := transport.JoinWorkspace(m.ps, m.self, name, s)
	if err != nil {
		_ = s.Close()
		return nil, nil, nil, fmt.Errorf("join topic %q: %w", name, err)
	}

	tracker, err := presence.New(ctx, m.ps, m.self, name)
	if err != nil {
		_ = s.Close()
		return nil, nil, nil, fmt.Errorf("presence for %q: %w", name, err)
	}

	m.active[name] = &entry{ws: ws, store: s, tracker: tracker}
	fmt.Printf("[workspace] joined channel: %s\n", name)
	return ws, s, tracker, nil
}

// List returns the names of all currently joined channels.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.active))
	for name := range m.active {
		names = append(names, name)
	}
	return names
}

// Get returns the workspace, store and tracker for a channel, or false if not joined.
func (m *Manager) Get(name string) (*transport.Workspace, *store.Store, *presence.Tracker, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.active[name]
	if !ok {
		return nil, nil, nil, false
	}
	return e.ws, e.store, e.tracker, true
}

// CloseAll shuts down all active channel stores.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, e := range m.active {
		_ = e.store.Close()
		delete(m.active, name)
	}
}
