package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/safecast/groove-go/internal/node"
	"github.com/safecast/groove-go/internal/transport"
	"github.com/safecast/groove-go/internal/web"
	"github.com/safecast/groove-go/internal/workspace"
)

func main() {
	port      := flag.Int("port", 0, "libp2p TCP listen port (0 = random)")
	httpAddr  := flag.String("http", ":8080", "Web UI listen address")
	defaultCh := flag.String("workspace", "general", "Default channel to join on startup")
	dataDir   := flag.String("data", defaultDataDir(), "Base directory for local stores")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Boot libp2p node
	n, err := node.New(ctx, *port)
	if err != nil {
		fatal(err)
	}
	defer n.Close()

	// mDNS LAN discovery
	mdnsSvc, err := node.StartMDNS(ctx, n.Host)
	if err != nil {
		fatal(err)
	}
	defer mdnsSvc.Close()

	// GossipSub router
	ps, err := transport.NewGossipSub(ctx, n.Host)
	if err != nil {
		fatal(err)
	}

	// Workspace manager — handles all channels
	mgr := workspace.New(ps, n.ID, *dataDir)
	defer mgr.CloseAll()

	// Join default channel on startup
	if _, _, err := mgr.Join(*defaultCh); err != nil {
		fatal(err)
	}

	getPeers := func(ws *transport.Workspace) int { return len(ws.ListPeers()) }

	webSrv := web.New(n.ID.String(), mgr, getPeers)
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
