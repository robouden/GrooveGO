package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/safecast/groove-go/internal/node"
)

func main() {
	port := flag.Int("port", 0, "TCP listen port (0 = random)")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n, err := node.New(ctx, *port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %s\n", err)
		os.Exit(1)
	}
	defer n.Close()

	mdnsSvc, err := node.StartMDNS(ctx, n.Host)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %s\n", err)
		os.Exit(1)
	}
	defer mdnsSvc.Close()

	fmt.Println("[groove] node running — press Ctrl+C to stop")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("[groove] shutting down")
}
