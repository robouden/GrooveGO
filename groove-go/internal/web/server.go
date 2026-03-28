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
	"github.com/safecast/groove-go/internal/presence"
	"github.com/safecast/groove-go/internal/store"
	"github.com/safecast/groove-go/internal/transport"
	"github.com/safecast/groove-go/internal/workspace"
)

//go:embed static
var staticFiles embed.FS

const maxUploadSize = 10 * 1024 * 1024 // 10 MB

// ── WebSocket envelope types ────────────────────────────────────────────────

type initEnv struct {
	Type       string          `json:"type"`
	SelfID     string          `json:"self_id"`
	Workspace  string          `json:"workspace"`
	History    []store.Message `json:"history"`
	Workspaces []string        `json:"workspaces"`
}
type msgEnv struct {
	Type      string        `json:"type"`
	Workspace string        `json:"workspace"`
	Message   store.Message `json:"message"`
}
type peersEnv struct {
	Type      string   `json:"type"`
	Workspace string   `json:"workspace"`
	Count     int      `json:"count"`
	Online    []string `json:"online"` // short peer IDs
}
type workspacesEnv struct {
	Type       string   `json:"type"`
	Workspaces []string `json:"workspaces"`
}

type clientMsg struct {
	Type      string `json:"type"`
	Body      string `json:"body"`
	Workspace string `json:"workspace"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server serves the GrooveGO web UI.
type Server struct {
	selfID string
	mgr    *workspace.Manager
}

// New creates a Server.
func New(selfID string, mgr *workspace.Manager) *Server {
	return &Server{selfID: selfID, mgr: mgr}
}

// ListenAndServe starts the HTTP server on addr.
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

// handleWS manages a single browser connection with channel-switching and presence.
func (srv *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	outerCtx, outerCancel := context.WithCancel(r.Context())
	defer outerCancel()

	defaultCh := r.URL.Query().Get("workspace")
	if defaultCh == "" {
		defaultCh = "general"
	}

	incoming := make(chan transport.Message, 64)

	// Browser reader goroutine
	browserCh := make(chan clientMsg, 16)
	go func() {
		defer outerCancel()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var cm clientMsg
			if json.Unmarshal(data, &cm) == nil {
				browserCh <- cm
			}
		}
	}()

	// switchTo joins a channel, sends init + presence, starts receive loop.
	var cancelRecv context.CancelFunc

	switchTo := func(name string) {
		if cancelRecv != nil {
			cancelRecv()
		}

		ws, s, tracker, err := srv.mgr.Join(outerCtx, name)
		if err != nil {
			srv.writeJSON(conn, map[string]string{"type": "error", "message": err.Error()})
			return
		}

		history, _ := s.History(name)
		if history == nil {
			history = []store.Message{}
		}
		srv.writeJSON(conn, initEnv{
			Type:       "init",
			SelfID:     srv.selfID,
			Workspace:  name,
			History:    history,
			Workspaces: srv.mgr.List(),
		})
		// Send initial presence
		srv.writeJSON(conn, srv.presenceEnv(name, tracker))

		recvCtx, cancel := context.WithCancel(outerCtx)
		cancelRecv = cancel
		go ws.ReadLoopInto(recvCtx, func(m transport.Message) {
			if m.MsgType == transport.MsgTypeFile && m.FileData != "" {
				if raw, decErr := base64.StdEncoding.DecodeString(m.FileData); decErr == nil {
					_, _ = s.SaveFile(m.FileID, m.FileName, raw)
				}
			}
			select {
			case incoming <- m:
			default:
			}
		})
	}

	switchTo(defaultCh)

	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-outerCtx.Done():
			return

		case cm := <-browserCh:
			switch cm.Type {
			case "send":
				if cm.Body == "" {
					continue
				}
				if ws, s, _, ok := srv.mgr.Get(defaultCh); ok {
					if err := ws.Publish(outerCtx, cm.Body); err == nil {
						srv.writeJSON(conn, msgEnv{
							Type:      "message",
							Workspace: defaultCh,
							Message: store.Message{
								MsgType:   transport.MsgTypeChat,
								From:      srv.selfID,
								Workspace: defaultCh,
								Body:      cm.Body,
								Timestamp: time.Now().UTC(),
							},
						})
						_ = s
					}
				}
			case "join":
				if cm.Workspace == "" {
					continue
				}
				defaultCh = cm.Workspace
				switchTo(cm.Workspace)
				srv.writeJSON(conn, workspacesEnv{
					Type:       "workspaces",
					Workspaces: srv.mgr.List(),
				})
			case "list":
				srv.writeJSON(conn, workspacesEnv{
					Type:       "workspaces",
					Workspaces: srv.mgr.List(),
				})
			}

		case m := <-incoming:
			srv.writeJSON(conn, msgEnv{
				Type:      "message",
				Workspace: m.Workspace,
				Message:   store.Message(m),
			})

		case <-tick.C:
			// Push peer count + online list
			if _, _, tracker, ok := srv.mgr.Get(defaultCh); ok {
				srv.writeJSON(conn, srv.presenceEnv(defaultCh, tracker))
			}
		}
	}
}

func (srv *Server) presenceEnv(wsName string, t *presence.Tracker) peersEnv {
	online := t.Online()
	ids := make([]string, len(online))
	for i, p := range online {
		s := p.ID.String()
		if len(s) > 12 {
			s = s[:12]
		}
		ids[i] = s
	}
	return peersEnv{
		Type:      "peers",
		Workspace: wsName,
		Count:     len(online),
		Online:    ids,
	}
}

// handleUpload receives a file and publishes it.
func (srv *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	wsName := r.URL.Query().Get("workspace")
	if wsName == "" {
		wsName = "general"
	}
	ws, s, _, ok := srv.mgr.Get(wsName)
	if !ok {
		http.Error(w, "not joined to workspace: "+wsName, http.StatusBadRequest)
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
	if err != nil || len(data) > maxUploadSize {
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

	if _, err := s.SaveFile(fileID, header.Filename, data); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	if err := ws.PublishFile(r.Context(), fileID, header.Filename, mimeType, data); err != nil {
		http.Error(w, "publish error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "file_id": fileID,
		"file_name": header.Filename, "file_size": len(data), "mime": mimeType,
	})
}

// handleFileDownload serves a saved file. URL: /files/{fileID}/{fileName}
func (srv *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	rest := r.URL.Path[len("/files/"):]
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
	fileID   := rest[:slash]
	fileName := rest[slash+1:]
	for _, name := range srv.mgr.List() {
		if _, s, _, ok := srv.mgr.Get(name); ok {
			path := s.FilePath(fileID, fileName)
			w.Header().Set("Content-Disposition",
				fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(fileName)))
			http.ServeFile(w, r, path)
			return
		}
	}
	http.NotFound(w, r)
}

func (srv *Server) writeJSON(conn *websocket.Conn, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, data)
}
