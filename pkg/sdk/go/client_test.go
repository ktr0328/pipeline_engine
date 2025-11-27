package gosdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/pipeline-engine/internal/engine"
)

func TestClientCreateJob(t *testing.T) {
	t.Parallel()

	var received engine.JobRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/v1/jobs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		defer r.Body.Close()

		resp := jobEnvelope{Job: engine.Job{ID: "job-create", PipelineType: received.PipelineType}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	job, err := client.CreateJob(context.Background(), engine.JobRequest{PipelineType: "demo"})
	if err != nil {
		t.Fatalf("CreateJob errored: %v", err)
	}
	if received.PipelineType != "demo" {
		t.Fatalf("request pipeline type mismatch: %+v", received)
	}
	if job == nil || job.ID != "job-create" {
		t.Fatalf("unexpected job response: %+v", job)
	}
}

func TestClientGetJobHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if _, err := client.GetJob(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestClientRerunAndCancelJob(t *testing.T) {
	t.Parallel()

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		resp := jobEnvelope{Job: engine.Job{ID: "job-" + strings.TrimPrefix(r.URL.Path, "/v1/jobs/")}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	if _, err := client.CancelJob(context.Background(), "job-1", "user"); err != nil {
		t.Fatalf("CancelJob failed: %v", err)
	}
	if _, err := client.RerunJob(context.Background(), "job-1", RerunRequest{ReuseUpstream: true}); err != nil {
		t.Fatalf("RerunJob failed: %v", err)
	}

	if len(paths) != 2 || paths[0] != "/v1/jobs/job-1/cancel" || paths[1] != "/v1/jobs/job-1/rerun" {
		t.Fatalf("unexpected request paths: %+v", paths)
	}
}

func TestClientStreamJobs(t *testing.T) {
	t.Parallel()

	ndjson := strings.Join([]string{
		`{"event":"job_queued","job_id":"job-stream","data":{"id":"job-stream","pipeline_type":"demo","pipeline_version":"v0"}}`,
		`{"event":"job_status","job_id":"job-stream","data":{"status":"running"}}`,
		`{"event":"job_completed","job_id":"job-stream","data":{"status":"succeeded"}}`,
		`{"event":"stream_finished","job_id":"job-stream","data":{"status":"succeeded"}}`,
	}, "\n") + "\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/jobs" || r.URL.RawQuery != "stream=true" {
			t.Fatalf("unexpected stream request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(ndjson))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	events, job, err := client.StreamJobs(context.Background(), engine.JobRequest{PipelineType: "demo"})
	if err != nil {
		t.Fatalf("StreamJobs failed: %v", err)
	}
	if job == nil || job.ID != "job-stream" {
		t.Fatalf("unexpected accepted job: %+v", job)
	}

	var got []engine.StreamingEvent
	for evt := range events {
		got = append(got, evt)
	}

	if len(got) != 3 {
		t.Fatalf("unexpected number of streaming events: %+v", got)
	}
	if got[0].Event != "job_status" || got[1].Event != "job_completed" || got[2].Event != "stream_finished" {
		t.Fatalf("unexpected events: %+v", got)
	}
}

func TestClientStreamJobByID(t *testing.T) {
	t.Parallel()

	ndjson := strings.Join([]string{
		`{"event":"job_status","job_id":"job-1","data":{"status":"running"}}`,
		`{"event":"job_completed","job_id":"job-1","data":{"status":"succeeded"}}`,
	}, "\n") + "\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/jobs/job-1/stream" || r.URL.Query().Get("after_seq") != "7" {
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(ndjson))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	events, err := client.StreamJobByID(context.Background(), "job-1", 7)
	if err != nil {
		t.Fatalf("StreamJobByID failed: %v", err)
	}

	var got []engine.StreamingEvent
	for evt := range events {
		got = append(got, evt)
	}
	if len(got) != 2 || got[0].Event != "job_status" {
		t.Fatalf("unexpected events: %+v", got)
	}
}

func TestClientUpsertProviderProfile(t *testing.T) {
	t.Parallel()

	var received engine.ProviderProfile
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/config/providers" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	profile := engine.ProviderProfile{
		ID:           "openai-main",
		Kind:         engine.ProviderOpenAI,
		BaseURI:      "https://api.openai.com/v1",
		DefaultModel: "gpt-4o-mini",
	}
	if err := client.UpsertProviderProfile(context.Background(), profile); err != nil {
		t.Fatalf("UpsertProviderProfile failed: %v", err)
	}
	if received.ID != profile.ID || received.DefaultModel != profile.DefaultModel {
		t.Fatalf("received profile mismatch: %+v", received)
	}
}
