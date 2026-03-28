package node

import (
	"context"
	"crypto/rand"
	"fmt"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Node wraps a libp2p host with GrooveGO-specific state.
type Node struct {
	Host host.Host
	ID   peer.ID
}

// New creates a libp2p host with a fresh Ed25519 identity and listens on
// a random TCP port. Call Close() when done.
func New(ctx context.Context, listenPort int) (*Node, error) {
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate identity: %w", err)
	}

	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort)

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(listenAddr),
		libp2p.EnableNATService(),
	)
	if err != nil {
		return nil, fmt.Errorf("create host: %w", err)
	}

	n := &Node{
		Host: h,
		ID:   h.ID(),
	}

	fmt.Printf("[node] peer ID : %s\n", n.ID)
	for _, addr := range h.Addrs() {
		fmt.Printf("[node] listening: %s/p2p/%s\n", addr, n.ID)
	}

	return n, nil
}

// Close shuts down the libp2p host.
func (n *Node) Close() error {
	return n.Host.Close()
}
