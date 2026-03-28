package workspace

import (
	"fmt"
	"path/filepath"
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/safecast/groove-go/internal/store"
	"github.com/safecast/groove-go/internal/transport"
)

// entry holds an active workspace topic and its local store.
type entry struct {
	ws    *transport.Workspace
	store *store.Store
}

// Manager owns multiple named workspace channels. It is safe for concurrent use.
type Manager struct {
	ps       *pubsub.PubSub
	self     peer.ID
	storeDir string // base dir; each channel gets storeDir/<name>

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
// The first call opens a Badger store and subscribes to the GossipSub topic.
func (m *Manager) Join(name string) (*transport.Workspace, *store.Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.active[name]; ok {
		return e.ws, e.store, nil
	}

	s, err := store.Open(filepath.Join(m.storeDir, name))
	if err != nil {
		return nil, nil, fmt.Errorf("open store for %q: %w", name, err)
	}

	ws, err := transport.JoinWorkspace(m.ps, m.self, name, s)
	if err != nil {
		_ = s.Close()
		return nil, nil, fmt.Errorf("join topic %q: %w", name, err)
	}

	m.active[name] = &entry{ws: ws, store: s}
	fmt.Printf("[workspace] joined channel: %s\n", name)
	return ws, s, nil
}

// List returns the names of all currently joined channels, sorted.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.active))
	for name := range m.active {
		names = append(names, name)
	}
	return names
}

// Get returns the workspace and store for a named channel, or false if not joined.
func (m *Manager) Get(name string) (*transport.Workspace, *store.Store, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.active[name]
	if !ok {
		return nil, nil, false
	}
	return e.ws, e.store, true
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
