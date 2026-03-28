package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
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

	// Load history to pre-populate the TUI
	history, err := s.History(*workspace)
	if err != nil {
		fatal(err)
	}
	histMsgs := make([]incomingMsg, len(history))
	for i, h := range history {
		histMsgs[i] = incomingMsg{from: h.From, body: h.Body, workspace: h.Workspace, ts: h.Timestamp}
	}

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

	// Join workspace topic (with store for persistence)
	ws, err := transport.JoinWorkspace(ps, n.ID, *workspace, s)
	if err != nil {
		fatal(err)
	}

	// Bridge pubsub → TUI via buffered channel
	incoming := make(chan incomingMsg, 64)
	go ws.ReadLoopInto(ctx, func(m transport.Message) {
		select {
		case incoming <- incomingMsg{from: m.From, body: m.Body, workspace: m.Workspace, ts: m.Timestamp}:
		default:
		}
	})

	getPeers := func() int { return len(ws.ListPeers()) }

	m := newModel(ctx, *workspace, n.ID.String(), incoming, getPeers, ws.Publish, histMsgs)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fatal(err)
	}
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
