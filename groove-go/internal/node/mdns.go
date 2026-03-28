package node

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

const mdnsServiceTag = "groove-go.local"

// notifee satisfies mdns.Notifee. It connects to any peer discovered on the LAN.
type notifee struct {
	ctx context.Context
	h   host.Host
}

func (n *notifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.h.ID() {
		return // skip self
	}
	fmt.Printf("[mdns] found peer: %s\n", pi.ID)
	if err := n.h.Connect(n.ctx, pi); err != nil {
		fmt.Printf("[mdns] connect failed: %s\n", err)
	}
}

// StartMDNS starts a local mDNS discovery service on the given host.
// Peers on the same LAN segment are discovered and connected automatically.
func StartMDNS(ctx context.Context, h host.Host) (mdns.Service, error) {
	svc := mdns.NewMdnsService(h, mdnsServiceTag, &notifee{ctx: ctx, h: h})
	if err := svc.Start(); err != nil {
		return nil, fmt.Errorf("mdns start: %w", err)
	}
	fmt.Printf("[mdns] service started (tag=%s)\n", mdnsServiceTag)
	return svc, nil
}
