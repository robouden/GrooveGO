package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/safecast/groove-go/internal/node"
	"github.com/safecast/groove-go/internal/transport"
	"github.com/safecast/groove-go/internal/web"
	"github.com/safecast/groove-go/internal/workspace"
)

// multiFlag lets a flag be specified multiple times: --relay addr1 --relay addr2
type multiFlag []string

func (m *multiFlag) String() string  { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	port      := flag.Int("port", 0, "libp2p TCP listen port (0 = random)")
	httpAddr  := flag.String("http", ":8080", "Web UI listen address")
	defaultCh := flag.String("workspace", "general", "Default channel to join on startup")
	dataDir   := flag.String("data", defaultDataDir(), "Base directory for local stores")
	enableDHT := flag.Bool("dht", true, "Enable Kademlia DHT for WAN peer discovery")

	var relayAddrs  multiFlag
	var bootstrapAddrs multiFlag
	flag.Var(&relayAddrs, "relay",
		"Static relay address (repeat for multiple):\n"+
			"  e.g. /ip4/1.2.3.4/tcp/4001/p2p/<peerID>")
	flag.Var(&bootstrapAddrs, "bootstrap",
		"DHT bootstrap peer (repeat for multiple; defaults to IPFS bootstrap nodes)")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Boot libp2p node with NAT traversal
	n, err := node.New(ctx, node.Config{
		ListenPort:   *port,
		StaticRelays: []string(relayAddrs),
	})
	if err != nil {
		fatal(err)
	}
	defer n.Close()

	// mDNS for LAN discovery
	mdnsSvc, err := node.StartMDNS(ctx, n.Host)
	if err != nil {
		fatal(err)
	}
	defer mdnsSvc.Close()

	// Kademlia DHT for WAN discovery
	if *enableDHT {
		if _, err := node.StartDHT(ctx, n.Host, []string(bootstrapAddrs)); err != nil {
			fmt.Fprintf(os.Stderr, "[dht] warning: %s\n", err)
			// non-fatal — LAN still works via mDNS
		}
	}

	ps, err := transport.NewGossipSub(ctx, n.Host)
	if err != nil {
		fatal(err)
	}

	mgr := workspace.New(ps, n.ID, *dataDir)
	defer mgr.CloseAll()

	if _, _, _, err := mgr.Join(ctx, *defaultCh); err != nil {
		fatal(err)
	}

	webSrv := web.New(n.ID.String(), mgr)
	go func() {
		if err := webSrv.ListenAndServe(ctx, *httpAddr); err != nil {
			fmt.Fprintf(os.Stderr, "[web] %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Println("[groove-web] shutting down")
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".groove-data"
	}
	return filepath.Join(home, ".groove")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %s\n", err)
	os.Exit(1)
}
