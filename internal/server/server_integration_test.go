package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/server"
	"github.com/example/pipeline-engine/internal/store"
)

func TestServer_CreateJobAndGet(t *testing.T) {
	jobStore := store.NewMemoryStore()
	eng := engine.NewBasicEngine(jobStore)
	srv := server.NewServer(eng)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload := `{"pipeline_type":"demo","input":{"sources":[]}}`
	resp, err := http.Post(ts.URL+"/v1/jobs", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("failed to post job: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var jobResp struct {
		Job *engine.Job `json:"job"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
		t.Fatalf("failed to decode job response: %v", err)
	}
	if jobResp.Job == nil {
		t.Fatal("job response is nil")
	}

	getResp, err := http.Get(ts.URL + "/v1/jobs/" + jobResp.Job.ID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get status: %d", getResp.StatusCode)
	}
}

func TestServer_StreamJobUsingHTTPTestServer(t *testing.T) {
	jobStore := store.NewMemoryStore()
	eng := engine.NewBasicEngine(jobStore)
	srv := server.NewServer(eng)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"pipeline_type":"demo","input":{"sources":[]}}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/jobs?stream=true", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to create stream request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected stream status: %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	if !scanner.Scan() {
		t.Fatal("expected first job event")
	}
	first := scanner.Bytes()
	if !bytes.Contains(first, []byte("job_queued")) {
		t.Fatalf("first stream event is not job_queued: %s", string(first))
	}

	events := 1
	for scanner.Scan() {
		events++
		if events > 10 {
			break
		}
	}

	if err := scanner.Err(); err != nil && err != context.Canceled {
		t.Fatalf("stream read error: %v", err)
	}
	if events < 2 {
		t.Fatalf("expected multiple events, got %d", events)
	}
}
