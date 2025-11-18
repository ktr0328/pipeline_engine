package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/server"
	"github.com/example/pipeline-engine/internal/store"
)

func TestHandlerHealth(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	handler := server.NewHandler(&stubEngine{})
	handler.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("/health のステータスコードが不正です: %d", resp.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("レスポンスの JSON 解析に失敗しました: %v", err)
	}

	if payload["status"] != "ok" {
		t.Fatalf("/health のレスポンスが想定外です: %+v", payload)
	}
}

func TestHandlerCreateJob(t *testing.T) {
	t.Parallel()

	var received engine.JobRequest
	stub := &stubEngine{
		runJobFunc: func(ctx context.Context, req engine.JobRequest) (*engine.Job, error) {
			received = req
			job := minimalJob("job-123")
			job.PipelineType = req.PipelineType
			return job, nil
		},
	}

	mux := http.NewServeMux()
	handler := server.NewHandler(stub)
	handler.Register(mux)

	body := bytes.NewBufferString(`{"pipeline_type":"demo","input":{"sources":[]}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", body)
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("/v1/jobs のステータスコードが不正です: %d", resp.Code)
	}

	if received.PipelineType != engine.PipelineType("demo") {
		t.Fatalf("Handler が受け取った pipeline_type が不正です: %s", received.PipelineType)
	}

	var payload struct {
		Job *engine.Job `json:"job"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("レスポンスの JSON 解析に失敗しました: %v", err)
	}

	if payload.Job == nil || payload.Job.ID != "job-123" {
		t.Fatalf("レスポンスの job 情報が不正です: %+v", payload.Job)
	}
}

func TestHandlerCancelJob(t *testing.T) {
	t.Parallel()

	cancelled := false
	var cancelReason string
	stub := &stubEngine{
		cancelJobFunc: func(ctx context.Context, jobID string, reason string) error {
			if jobID != "job-55" {
				t.Fatalf("cancel 対象の jobID が不正です: %s", jobID)
			}
			cancelled = true
			cancelReason = reason
			return nil
		},
		getJobFunc: func(ctx context.Context, jobID string) (*engine.Job, error) {
			job := minimalJob(jobID)
			job.Status = engine.JobStatusCancelled
			job.Error = &engine.JobError{Code: "cancelled", Message: "user"}
			return job, nil
		},
	}

	mux := http.NewServeMux()
	handler := server.NewHandler(stub)
	handler.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/jobs/job-55/cancel", strings.NewReader(`{"reason":"user aborted"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("/v1/jobs/{id}/cancel のステータスコードが不正です: %d", resp.Code)
	}
	if !cancelled {
		t.Fatal("CancelJob が呼び出されていません")
	}
	if cancelReason != "user aborted" {
		t.Fatalf("キャンセル理由が伝搬していません: %s", cancelReason)
	}

	var payload struct {
		Job *engine.Job `json:"job"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("レスポンスの JSON 解析に失敗しました: %v", err)
	}
	if payload.Job == nil || payload.Job.Status != engine.JobStatusCancelled {
		t.Fatalf("キャンセル後のジョブが想定外です: %+v", payload.Job)
	}
}

func TestHandlerGetJobNotFound(t *testing.T) {
	t.Parallel()

	stub := &stubEngine{
		getJobFunc: func(ctx context.Context, jobID string) (*engine.Job, error) {
			return nil, store.ErrJobNotFound
		},
	}

	mux := http.NewServeMux()
	handler := server.NewHandler(stub)
	handler.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/unknown", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("存在しないジョブのステータスコードが不正です: %d", resp.Code)
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("エラーレスポンスの JSON 解析に失敗しました: %v", err)
	}
	if payload.Error.Code != "not_found" {
		t.Fatalf("エラーコードが not_found ではありません: %+v", payload)
	}
}

type stubEngine struct {
	runJobFunc       func(ctx context.Context, req engine.JobRequest) (*engine.Job, error)
	runJobStreamFunc func(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error)
	cancelJobFunc    func(ctx context.Context, jobID string, reason string) error
	getJobFunc       func(ctx context.Context, jobID string) (*engine.Job, error)
}

func (s *stubEngine) RunJob(ctx context.Context, req engine.JobRequest) (*engine.Job, error) {
	if s.runJobFunc == nil {
		return nil, errors.New("runJob not implemented")
	}
	return s.runJobFunc(ctx, req)
}

func (s *stubEngine) RunJobStream(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error) {
	if s.runJobStreamFunc == nil {
		return nil, nil, errors.New("runJobStream not implemented")
	}
	return s.runJobStreamFunc(ctx, req)
}

func (s *stubEngine) CancelJob(ctx context.Context, jobID string, reason string) error {
	if s.cancelJobFunc == nil {
		return errors.New("cancelJob not implemented")
	}
	return s.cancelJobFunc(ctx, jobID, reason)
}

func (s *stubEngine) GetJob(ctx context.Context, jobID string) (*engine.Job, error) {
	if s.getJobFunc == nil {
		return nil, errors.New("getJob not implemented")
	}
	return s.getJobFunc(ctx, jobID)
}

func minimalJob(id string) *engine.Job {
	now := time.Now().UTC()
	return &engine.Job{
		ID:           id,
		PipelineType: engine.PipelineType("demo"),
		Status:       engine.JobStatusQueued,
		CreatedAt:    now,
		UpdatedAt:    now,
		Input: engine.JobInput{
			Sources: []engine.Source{{Kind: engine.SourceKindNote, Label: "memo", Content: "test"}},
		},
	}
}
