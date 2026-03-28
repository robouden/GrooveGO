package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/safecast/groove-go/internal/node"
	"github.com/safecast/groove-go/internal/store"
	"github.com/safecast/groove-go/internal/transport"
)

func main() {
	port      := flag.Int("port", 0, "TCP listen port (0 = random)")
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

	// Replay history from previous sessions
	history, err := s.History(*workspace)
	if err != nil {
		fatal(err)
	}
	if len(history) > 0 {
		fmt.Printf("[store] replaying %d message(s) from previous session:\n", len(history))
		for _, m := range history {
			fmt.Printf("  [%s] %s: %s\n", m.Workspace, shortID(m.From), m.Body)
		}
		fmt.Println("[store] --- end of history ---")
	}

	// Boot libp2p node
	n, err := node.New(ctx, node.Config{ListenPort: *port})
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

	// Join workspace topic (with store for persistence)
	ws, err := transport.JoinWorkspace(ps, n.ID, *workspace, s)
	if err != nil {
		fatal(err)
	}

	// Receive messages in background
	go ws.ReadLoop(ctx)

	fmt.Printf("[groove] workspace: %s — type a message and press Enter\n", *workspace)
	fmt.Println("[groove] Ctrl+C to quit")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	scanner := bufio.NewScanner(os.Stdin)
	input   := make(chan string)

	go func() {
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				input <- line
			}
		}
		close(input)
	}()

	for {
		select {
		case line, ok := <-input:
			if !ok {
				return
			}
			if err := ws.Publish(ctx, line); err != nil {
				fmt.Fprintf(os.Stderr, "[error] publish: %s\n", err)
			} else {
				fmt.Printf("[%s] me: %s\n", *workspace, line)
			}
		case <-quit:
			fmt.Println("[groove] shutting down")
			return
		}
	}
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".groove-data"
	}
	return filepath.Join(home, ".groove")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %s\n", err)
	os.Exit(1)
}
