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
	"github.com/safecast/groove-go/internal/store"
	"github.com/safecast/groove-go/internal/transport"
	"github.com/safecast/groove-go/internal/web"
)

func main() {
	port      := flag.Int("port", 0, "libp2p TCP listen port (0 = random)")
	httpAddr  := flag.String("http", ":8080", "Web UI listen address")
	workspace := flag.String("workspace", "general", "Workspace name to join")
	dataDir   := flag.String("data", defaultDataDir(), "Directory for local message store")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Local persistence
	s, err := store.Open(filepath.Join(*dataDir, *workspace))
	if err != nil {
		fatal(err)
	}
	defer s.Close()

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

	// Join workspace topic
	ws, err := transport.JoinWorkspace(ps, n.ID, *workspace, s)
	if err != nil {
		fatal(err)
	}

	getPeers := func() int { return len(ws.ListPeers()) }

	// Start web server
	webSrv := web.New(n.ID.String(), *workspace, ws, s, getPeers)
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
