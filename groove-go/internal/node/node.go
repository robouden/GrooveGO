package node

import (
	"context"
	"crypto/rand"
	"fmt"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// Config controls optional NAT traversal behaviour.
type Config struct {
	// ListenPort is the TCP port to listen on (0 = random).
	ListenPort int
	// StaticRelays are circuit-relay v2 nodes to use when behind NAT.
	// Format: /ip4/<addr>/tcp/<port>/p2p/<peerID>
	StaticRelays []string
}

// Node wraps a libp2p host with GrooveGO-specific state.
type Node struct {
	Host host.Host
	ID   peer.ID
}

// New creates a libp2p host with an Ed25519 identity, NAT traversal, and
// optional static relay support.
func New(ctx context.Context, cfg Config) (*Node, error) {
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate identity: %w", err)
	}

	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.ListenPort)

	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(
			listenAddr,
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", cfg.ListenPort),
		),
		libp2p.EnableNATService(),  // tell peers our observed public address
		libp2p.NATPortMap(),        // UPnP / NAT-PMP port mapping
		libp2p.EnableHolePunching(), // DCUtR direct connection upgrade
	}

	// AutoRelay: use supplied static relays or fall back to none.
	if len(cfg.StaticRelays) > 0 {
		relays, err := parseRelays(cfg.StaticRelays)
		if err != nil {
			return nil, fmt.Errorf("parse relay addrs: %w", err)
		}
		opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays(relays))
		fmt.Printf("[node] auto-relay enabled with %d static relay(s)\n", len(relays))
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create host: %w", err)
	}

	n := &Node{Host: h, ID: h.ID()}

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

func parseRelays(addrs []string) ([]peer.AddrInfo, error) {
	out := make([]peer.AddrInfo, 0, len(addrs))
	for _, s := range addrs {
		ma, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			return nil, fmt.Errorf("invalid relay addr %q: %w", s, err)
		}
		ai, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			return nil, fmt.Errorf("relay addr info %q: %w", s, err)
		}
		out = append(out, *ai)
	}
	return out, nil
}
