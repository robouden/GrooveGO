// groove-relay is a minimal libp2p circuit relay + DHT bootstrap node
// intended to run on a public VPS. It has no UI — it just keeps running,
// relaying traffic and helping peers find each other across the internet.
//
// Usage:
//
//	groove-relay --port 4001
//
// On startup it prints its full multiaddr. Give that address to groove-web
// clients via --relay to let them traverse NAT.
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
)

func main() {
	port := flag.Int("port", 4001, "TCP listen port")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		fatal(err)
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", *port),
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", *port),
		),
		libp2p.EnableRelayService(), // circuit relay v2
		libp2p.EnableNATService(),
	)
	if err != nil {
		fatal(err)
	}
	defer h.Close()

	// Run DHT in server mode — acts as a bootstrap node for WAN discovery.
	kadDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		fatal(err)
	}
	if err := kadDHT.Bootstrap(ctx); err != nil {
		fatal(err)
	}

	fmt.Println("[relay] node running — share these addresses with your peers:")
	for _, addr := range h.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", addr, h.ID())
	}
	fmt.Println("[relay] Ctrl+C to stop")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Println("[relay] shutting down")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %s\n", err)
	os.Exit(1)
}
