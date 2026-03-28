package web

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/safecast/groove-go/internal/store"
	"github.com/safecast/groove-go/internal/transport"
)

//go:embed static
var staticFiles embed.FS

const maxUploadSize = 10 * 1024 * 1024 // 10 MB

// WebSocket envelope types sent to the browser.
type initEnv struct {
	Type      string          `json:"type"`
	SelfID    string          `json:"self_id"`
	Workspace string          `json:"workspace"`
	History   []store.Message `json:"history"`
}
type msgEnv struct {
	Type    string        `json:"type"`
	Message store.Message `json:"message"`
}
type peersEnv struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}
type errEnv struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// clientSend is what the browser sends us over WebSocket.
type clientSend struct {
	Type string `json:"type"` // "send"
	Body string `json:"body"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server serves the GrooveGO web UI and bridges WebSocket + file HTTP to pubsub.
type Server struct {
	selfID    string
	workspace string
	ws        *transport.Workspace
	store     *store.Store
	getPeers  func() int
}

// New creates a Server.
func New(selfID, workspace string, ws *transport.Workspace, s *store.Store, getPeers func() int) *Server {
	return &Server{selfID: selfID, workspace: workspace, ws: ws, store: s, getPeers: getPeers}
}

// ListenAndServe starts the HTTP server on addr (e.g. ":8080").
func (srv *Server) ListenAndServe(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/ws", srv.handleWS)
	mux.HandleFunc("/upload", srv.handleUpload)
	mux.HandleFunc("/files/", srv.handleFileDownload)

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

// handleUpload accepts a multipart file POST, publishes it over pubsub, saves locally.
func (srv *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "file too large (max 10 MB)", http.StatusRequestEntityTooLarge)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadSize+1))
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	if len(data) > maxUploadSize {
		http.Error(w, "file too large (max 10 MB)", http.StatusRequestEntityTooLarge)
		return
	}

	fileID := uuid.New().String()
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Save locally
	if _, err := srv.store.SaveFile(fileID, header.Filename, data); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	// Publish to peers
	if err := srv.ws.PublishFile(r.Context(), fileID, header.Filename, mimeType, data); err != nil {
		http.Error(w, "publish error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":        true,
		"file_id":   fileID,
		"file_name": header.Filename,
		"file_size": len(data),
		"mime":      mimeType,
	})
}

// handleFileDownload serves a previously received file.
// URL pattern: /files/{fileID}/{fileName}
func (srv *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	// strip "/files/" prefix
	rest := r.URL.Path[len("/files/"):]
	// split into fileID / fileName
	slash := len(rest)
	for i, c := range rest {
		if c == '/' {
			slash = i
			break
		}
	}
	if slash >= len(rest) {
		http.NotFound(w, r)
		return
	}
	fileID  := rest[:slash]
	fileName := rest[slash+1:]
	path := srv.store.FilePath(fileID, fileName)

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(fileName)))
	http.ServeFile(w, r, path)
}

// handleWS upgrades the connection and bridges pubsub ↔ browser.
func (srv *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

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

	// Incoming pubsub messages → browser
	incoming := make(chan transport.Message, 32)
	go srv.ws.ReadLoopInto(ctx, func(m transport.Message) {
		// Save file to disk when received
		if m.MsgType == transport.MsgTypeFile && m.FileData != "" {
			if raw, err := base64.StdEncoding.DecodeString(m.FileData); err == nil {
				_, _ = srv.store.SaveFile(m.FileID, m.FileName, raw)
			}
		}
		select {
		case incoming <- m:
		default:
		}
	})

	peerTick := time.NewTicker(2 * time.Second)
	defer peerTick.Stop()

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
				srv.writeJSON(conn, msgEnv{
					Type: "message",
					Message: store.Message{
						MsgType:   transport.MsgTypeChat,
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
