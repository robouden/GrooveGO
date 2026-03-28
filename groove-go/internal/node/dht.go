package node

import (
	"context"
	"fmt"
	"sync"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// DefaultBootstrapPeers are well-known IPFS bootstrap nodes used as DHT
// entry points when no custom bootstrap peers are provided.
var DefaultBootstrapPeers = []string{
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
}

// StartDHT creates a Kademlia DHT in client mode, bootstraps it, and
// returns it. The DHT enables WAN peer discovery beyond the local network.
//
// bootstrapAddrs overrides DefaultBootstrapPeers when non-empty.
func StartDHT(ctx context.Context, h host.Host, bootstrapAddrs []string) (*dht.IpfsDHT, error) {
	if len(bootstrapAddrs) == 0 {
		bootstrapAddrs = DefaultBootstrapPeers
	}

	kadDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeClient))
	if err != nil {
		return nil, fmt.Errorf("dht create: %w", err)
	}

	if err := kadDHT.Bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("dht bootstrap: %w", err)
	}

	// Connect to bootstrap peers in parallel; ignore individual failures.
	var wg sync.WaitGroup
	connected := 0
	var mu sync.Mutex

	for _, addrStr := range bootstrapAddrs {
		addrStr := addrStr
		wg.Add(1)
		go func() {
			defer wg.Done()
			ma, err := multiaddr.NewMultiaddr(addrStr)
			if err != nil {
				return
			}
			ai, err := peer.AddrInfoFromP2pAddr(ma)
			if err != nil {
				return
			}
			if err := h.Connect(ctx, *ai); err != nil {
				return
			}
			mu.Lock()
			connected++
			mu.Unlock()
		}()
	}
	wg.Wait()

	fmt.Printf("[dht] bootstrapped (%d/%d peers reached)\n", connected, len(bootstrapAddrs))
	return kadDHT, nil
}
