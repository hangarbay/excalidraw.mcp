// Package httpserver serves the embedded Excalidraw editor and its scene API.
package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hangarbay/excalidraw.mcp/internal/scene"
	"github.com/hangarbay/excalidraw.mcp/internal/webui"
)

const maxSceneBytes = 32 << 20

type Server struct {
	store   *scene.Store
	handler http.Handler
}

type replaceRequest struct {
	Scene            scene.Scene `json:"scene"`
	ExpectedRevision *uint64     `json:"expectedRevision,omitempty"`
	ClientID         string      `json:"clientId,omitempty"`
}

func New(store *scene.Store) *Server {
	return newServer(store, nil)
}

// NewWithMCP serves the browser application, scene API, and a Streamable HTTP
// MCP endpoint from one process.
func NewWithMCP(store *scene.Store, mcpHandler http.Handler) *Server {
	return newServer(store, mcpHandler)
}

func newServer(store *scene.Store, mcpHandler http.Handler) *Server {
	s := &Server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/scene", s.getScene)
	mux.HandleFunc("PUT /api/scene", s.replaceScene)
	mux.HandleFunc("GET /api/events", s.events)
	if mcpHandler != nil {
		mux.Handle("/mcp", mcpHandler)
	}
	mux.Handle("/", webui.Handler())
	s.handler = securityHeaders(mux)
	return s
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getScene(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.store.Snapshot())
}

func (s *Server) replaceScene(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxSceneBytes))
	var input replaceRequest
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid scene JSON: "+err.Error())
		return
	}
	if err := ensureEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	source := input.ClientID
	if source == "" {
		source = "browser"
	}
	snapshot, err := s.store.Replace(input.Scene, input.ExpectedRevision, source)
	if errors.Is(err, scene.ErrRevisionConflict) {
		writeJSON(w, http.StatusConflict, s.store.Snapshot())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events, unsubscribe := s.store.Subscribe()
	defer unsubscribe()
	if err := writeEvent(w, scene.Event{Snapshot: s.store.Snapshot(), Source: "server"}); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			if err := writeEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeEvent(w io.Writer, event scene.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: scene\ndata: %s\n\n", data)
	return err
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request must contain exactly one JSON object")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
