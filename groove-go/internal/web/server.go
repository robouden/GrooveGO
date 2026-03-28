package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/safecast/groove-go/internal/store"
	"github.com/safecast/groove-go/internal/transport"
)

//go:embed static
var staticFiles embed.FS

// Envelope types sent over WebSocket to the browser.
type initEnv struct {
	Type      string          `json:"type"`      // "init"
	SelfID    string          `json:"self_id"`
	Workspace string          `json:"workspace"`
	History   []store.Message `json:"history"`
}

type msgEnv struct {
	Type    string          `json:"type"` // "message"
	Message store.Message   `json:"message"`
}

type peersEnv struct {
	Type  string `json:"type"` // "peers"
	Count int    `json:"count"`
}

// clientSend is what the browser sends to us.
type clientSend struct {
	Type string `json:"type"` // "send"
	Body string `json:"body"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // allow all origins for local use
}

// Server serves the GrooveGO web UI and bridges WebSocket clients to pubsub.
type Server struct {
	selfID    string
	workspace string
	ws        *transport.Workspace
	store     *store.Store
	getPeers  func() int
}

// New creates a Server. Call ListenAndServe to start it.
func New(selfID, workspace string, ws *transport.Workspace, s *store.Store, getPeers func() int) *Server {
	return &Server{
		selfID:    selfID,
		workspace: workspace,
		ws:        ws,
		store:     s,
		getPeers:  getPeers,
	}
}

// ListenAndServe starts the HTTP server on addr (e.g. ":8080").
func (srv *Server) ListenAndServe(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	// Serve static files from embedded FS
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/ws", srv.handleWS)

	httpSrv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()

	fmt.Printf("[web] UI available at http://localhost%s\n", addr)
	if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (srv *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Send init frame with history
	history, _ := srv.store.History(srv.workspace)
	if history == nil {
		history = []store.Message{}
	}
	srv.writeJSON(conn, initEnv{
		Type:      "init",
		SelfID:    srv.selfID,
		Workspace: srv.workspace,
		History:   history,
	})

	// Pump incoming pubsub messages → browser
	incoming := make(chan transport.Message, 32)
	go srv.ws.ReadLoopInto(ctx, func(m transport.Message) {
		select {
		case incoming <- m:
		default:
		}
	})

	// Periodic peer count
	peerTick := time.NewTicker(2 * time.Second)
	defer peerTick.Stop()

	// Read from browser in separate goroutine
	browserMsgs := make(chan clientSend, 8)
	browserDone := make(chan struct{})
	go func() {
		defer close(browserDone)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			var cm clientSend
			if err := json.Unmarshal(data, &cm); err == nil && cm.Type == "send" && cm.Body != "" {
				browserMsgs <- cm
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-browserDone:
			return

		case cm := <-browserMsgs:
			if err := srv.ws.Publish(ctx, cm.Body); err == nil {
				// Echo back to this browser client as a regular message
				srv.writeJSON(conn, msgEnv{
					Type: "message",
					Message: store.Message{
						From:      srv.selfID,
						Workspace: srv.workspace,
						Body:      cm.Body,
						Timestamp: time.Now().UTC(),
					},
				})
			}

		case m := <-incoming:
			srv.writeJSON(conn, msgEnv{
				Type:    "message",
				Message: store.Message(m),
			})

		case <-peerTick.C:
			srv.writeJSON(conn, peersEnv{Type: "peers", Count: srv.getPeers()})
		}
	}
}

func (srv *Server) writeJSON(conn *websocket.Conn, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, data)
}
