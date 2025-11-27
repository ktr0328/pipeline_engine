package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

type MockServer struct {
	Server *httptest.Server
	Events []map[string]any
}

func NewMockServer() *MockServer {
	ms := &MockServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Query().Get("stream") == "true" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			enc := json.NewEncoder(w)
			events := []map[string]any{
				{"event": "job_queued", "job_id": "mock", "data": map[string]string{"id": "mock"}},
				{"event": "job_status", "job_id": "mock", "data": map[string]string{"status": "running"}},
				{"event": "stream_finished", "job_id": "mock", "data": map[string]string{"status": "succeeded"}},
			}
			for _, evt := range events {
				_ = enc.Encode(evt)
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job": map[string]any{
				"id":            "mock",
				"pipeline_type": "mock",
				"status":        "queued",
			},
		})
	})
	ms.Server = httptest.NewServer(mux)
	return ms
}

func (m *MockServer) Close() {
	if m.Server != nil {
		m.Server.Close()
	}
}
