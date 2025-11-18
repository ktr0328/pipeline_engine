package server

import (
	"context"
	"net/http"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
)

// Version represents the server version exposed via /health.
const Version = "0.2.0"

// Server is a minimal HTTP server that exposes engine capabilities.
type Server struct {
	engine     engine.Engine
	mux        *http.ServeMux
	startedAt  time.Time
	version    string
	httpServer *http.Server
}

// NewServer wires the HTTP handlers and returns a Server instance.
func NewServer(e engine.Engine) *Server {
	started := time.Now().UTC()
	mux := http.NewServeMux()
	handler := NewHandler(e, started, Version)
	handler.Register(mux)
	return &Server{engine: e, mux: mux, startedAt: started, version: Version}
}

// ListenAndServe starts listening on the provided address.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	s.httpServer = srv
	return srv.ListenAndServe()
}

// Handler exposes the HTTP handler, making it easier to embed the server elsewhere.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// Shutdown gracefully stops the underlying HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}
