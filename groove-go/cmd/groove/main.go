package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/safecast/groove-go/internal/node"
	"github.com/safecast/groove-go/internal/transport"
)

func main() {
	port      := flag.Int("port", 0, "TCP listen port (0 = random)")
	workspace := flag.String("workspace", "general", "Workspace name to join")
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

	// Join workspace topic
	ws, err := transport.JoinWorkspace(ps, n.ID, *workspace)
	if err != nil {
		fatal(err)
	}

	// Receive messages in background
	go ws.ReadLoop(ctx)

	fmt.Printf("[groove] type a message and press Enter to send (workspace: %s)\n", *workspace)
	fmt.Println("[groove] Ctrl+C to quit")

	// Read stdin and publish
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

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %s\n", err)
	os.Exit(1)
}
