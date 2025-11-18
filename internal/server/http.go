package server

import (
	"net/http"

	"github.com/example/pipeline-engine/internal/engine"
)

// Server is a minimal HTTP server that exposes engine capabilities.
type Server struct {
	engine engine.Engine
	mux    *http.ServeMux
}

// NewServer wires the HTTP handlers and returns a Server instance.
func NewServer(e engine.Engine) *Server {
	mux := http.NewServeMux()
	handler := NewHandler(e)
	handler.Register(mux)
	return &Server{engine: e, mux: mux}
}

// ListenAndServe starts listening on the provided address.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	return srv.ListenAndServe()
}

// Handler exposes the HTTP handler, making it easier to embed the server elsewhere.
func (s *Server) Handler() http.Handler {
	return s.mux
}
