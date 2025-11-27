package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/server"
	"github.com/example/pipeline-engine/internal/store"
	"github.com/example/pipeline-engine/pkg/metrics"
)

func TestHandlerHealth(t *testing.T) {
	t.Parallel()

	mux := newTestMux(&stubEngine{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	assertStatus(t, resp.Code, http.StatusOK)

	var payload map[string]interface{}
	decodeJSON(t, resp.Body.Bytes(), &payload)
	if payload["status"] != "ok" {
		t.Fatalf("/health の status が想定外です: %+v", payload)
	}
	if payload["version"] != "test-version" {
		t.Fatalf("/health の version が想定外です: %+v", payload)
	}
	if _, ok := payload["uptime_sec"]; !ok {
		t.Fatalf("uptime_sec フィールドが存在しません: %+v", payload)
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
	mux := newTestMux(stub)

	body := bytes.NewBufferString(`{"pipeline_type":"demo","input":{"sources":[]}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", body)
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	assertStatus(t, resp.Code, http.StatusAccepted)

	if received.PipelineType != engine.PipelineType("demo") {
		t.Fatalf("Handler が受け取った pipeline_type が不正です: %s", received.PipelineType)
	}

	var payload struct {
		Job *engine.Job `json:"job"`
	}
	decodeJSON(t, resp.Body.Bytes(), &payload)

	if payload.Job == nil || payload.Job.ID != "job-123" {
		t.Fatalf("レスポンスの job 情報が不正です: %+v", payload.Job)
	}
}

func TestHandlerCreateJobStream(t *testing.T) {
	t.Parallel()

	evCh := make(chan engine.StreamingEvent, 3)
	evCh <- engine.StreamingEvent{Event: "job_status", JobID: "job-stream", Data: minimalJob("job-stream")}
	evCh <- engine.StreamingEvent{Event: "job_completed", JobID: "job-stream", Data: minimalJob("job-stream")}
	evCh <- engine.StreamingEvent{Event: "stream_finished", JobID: "job-stream", Data: minimalJob("job-stream")}
	close(evCh)

	stub := &stubEngine{
		runJobStreamFunc: func(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error) {
			job := minimalJob("job-stream")
			job.Status = engine.JobStatusQueued
			return evCh, job, nil
		},
	}

	mux := newTestMux(stub)

	body := bytes.NewBufferString(`{"pipeline_type":"demo","input":{"sources":[]}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs?stream=true", body)
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("stream=true の /v1/jobs のステータスコードが不正です: %d", resp.Code)
	}

	var events []engine.StreamingEvent
	dec := json.NewDecoder(resp.Body)
	for {
		var evt engine.StreamingEvent
		if err := dec.Decode(&evt); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("NDJSON の解析に失敗しました: %v", err)
		}
		events = append(events, evt)
	}

	if len(events) != 4 {
		t.Fatalf("受信したイベント数が想定外です: %+v", events)
	}
	if events[0].Event != "job_queued" {
		t.Fatalf("最初のイベントが job_queued ではありません: %+v", events[0])
	}
	if events[1].Event != "job_status" || events[2].Event != "job_completed" || events[3].Event != "stream_finished" {
		t.Fatalf("ストリーミングイベントが期待と異なります: %+v", events)
	}
}

func TestHandlerStreamExistingJobAfterSeq(t *testing.T) {
	t.Parallel()

	job := minimalJob("job-resume")
	job.Status = engine.JobStatusSucceeded
	job.StepExecutions = []engine.StepExecution{{StepID: engine.StepID("step-1"), Status: engine.StepExecSuccess}}

	stub := &stubEngine{
		getJobFunc: func(ctx context.Context, jobID string) (*engine.Job, error) {
			if jobID != "job-resume" {
				t.Fatalf("unexpected job id: %s", jobID)
			}
			return job, nil
		},
	}

	mux := newTestMux(stub)

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs/job-resume/stream?after_seq=1", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("/v1/jobs/{id}/stream のステータスコードが不正です: %d", resp.Code)
	}

	dec := json.NewDecoder(resp.Body)
	var events []engine.StreamingEvent
	for {
		var evt engine.StreamingEvent
		if err := dec.Decode(&evt); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("resume stream decode error: %v", err)
		}
		events = append(events, evt)
	}

	if len(events) == 0 {
		t.Fatalf("再開後にイベントが取得できませんでした: %+v", events)
	}
	for _, evt := range events {
		if evt.Seq <= 1 {
			t.Fatalf("seq が after_seq 以下のイベントが含まれています: %+v", evt)
		}
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

	mux := newTestMux(stub)

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

	mux := newTestMux(stub)

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

func TestHandlerUpsertProviderProfile(t *testing.T) {
	t.Parallel()
	var received engine.ProviderProfile
	stub := &stubEngine{
		upsertProfileFunc: func(p engine.ProviderProfile) error {
			received = p
			return nil
		},
	}
	mux := newTestMux(stub)
	req := httptest.NewRequest(http.MethodPost, "/v1/config/providers", strings.NewReader(`{"id":"ts-sdk","kind":"openai","base_uri":"http://mock","api_key":"sk","default_model":"gpt"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	assertStatus(t, resp.Code, http.StatusOK)
	if received.ID != "ts-sdk" || received.Kind != engine.ProviderOpenAI {
		t.Fatalf("profile not passed to engine: %+v", received)
	}
}

func TestHandlerUpsertProviderProfileInvalidPayload(t *testing.T) {
	t.Parallel()
	stub := &stubEngine{}
	mux := newTestMux(stub)
	req := httptest.NewRequest(http.MethodPost, "/v1/config/providers", strings.NewReader(`{"kind":"openai"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when id missing, got %d", resp.Code)
	}
}

func TestHandlerUpdateEngineConfig(t *testing.T) {
	stor := store.NewMemoryStore()
	eng := engine.NewBasicEngine(stor)
	mux := newTestMux(eng)
	req := httptest.NewRequest(http.MethodPost, "/v1/config/engine", strings.NewReader(`{"log_level":"debug"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	assertStatus(t, resp.Code, http.StatusOK)
	var payload map[string]string
	decodeJSON(t, resp.Body.Bytes(), &payload)
	if payload["log_level"] != "debug" {
		t.Fatalf("expected response log_level debug, got %+v", payload)
	}
}

func TestHandlerUpdateEngineConfigRequiresValue(t *testing.T) {
	stor := store.NewMemoryStore()
	eng := engine.NewBasicEngine(stor)
	mux := newTestMux(eng)
	req := httptest.NewRequest(http.MethodPost, "/v1/config/engine", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no config provided, got %d", resp.Code)
	}
}

func TestHandlerRerunJob(t *testing.T) {
	t.Parallel()

	baseJob := minimalJob("job-base")
	baseJob.PipelineType = engine.PipelineType("log_summary")
	baseJob.Input = engine.JobInput{
		Sources: []engine.Source{{Kind: engine.SourceKindLog, Label: "log", Content: "line"}},
	}

	var captured engine.JobRequest
	stub := &stubEngine{
		getJobFunc: func(ctx context.Context, jobID string) (*engine.Job, error) {
			if jobID != "job-base" {
				t.Fatalf("想定外の jobID 取得: %s", jobID)
			}
			return baseJob, nil
		},
		runJobFunc: func(ctx context.Context, req engine.JobRequest) (*engine.Job, error) {
			captured = req
			resp := minimalJob("job-rerun")
			resp.ParentJobID = req.ParentJobID
			resp.Mode = req.Mode
			return resp, nil
		},
	}

	mux := newTestMux(stub)

	payload := `{
		"from_step_id": "step-2",
		"reuse_upstream": true,
		"override_input": {
			"sources": [{"kind":"note","label":"memo","content":"override"}]
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs/job-base/rerun", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("rerun のステータスコードが不正です: %d", resp.Code)
	}
	if captured.Mode != "rerun" {
		t.Fatalf("RunJob に渡した mode が rerun ではありません: %+v", captured)
	}
	if captured.ParentJobID == nil || *captured.ParentJobID != "job-base" {
		t.Fatalf("ParentJobID が設定されていません: %+v", captured.ParentJobID)
	}
	if captured.FromStepID == nil || *captured.FromStepID != engine.StepID("step-2") {
		t.Fatalf("FromStepID が設定されていません: %+v", captured.FromStepID)
	}
	if len(captured.Input.Sources) != 1 || captured.Input.Sources[0].Label != "memo" {
		t.Fatalf("override_input が反映されていません: %+v", captured.Input)
	}
	if !captured.ReuseUpstream {
		t.Fatalf("ReuseUpstream が true になっていません: %+v", captured)
	}

	var payloadResp struct {
		Job *engine.Job `json:"job"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payloadResp); err != nil {
		t.Fatalf("レスポンスの JSON 解析に失敗しました: %v", err)
	}
	if payloadResp.Job == nil || payloadResp.Job.ID != "job-rerun" {
		t.Fatalf("rerun レスポンスのジョブが想定外です: %+v", payloadResp.Job)
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	t.Parallel()

	mux := newTestMux(&stubEngine{})
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	assertStatus(t, resp.Code, http.StatusMethodNotAllowed)

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeJSON(t, resp.Body.Bytes(), &payload)
	if payload.Error.Code != "method_not_allowed" {
		t.Fatalf("method_not_allowed エラーが返りません: %+v", payload)
	}
}

func TestHandlerUnknownActionReturnsNotFound(t *testing.T) {
	t.Parallel()

	mux := newTestMux(&stubEngine{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs/job-123/unknown", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	assertStatus(t, resp.Code, http.StatusNotFound)

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeJSON(t, resp.Body.Bytes(), &payload)
	if payload.Error.Code != "not_found" {
		t.Fatalf("not_found エラーが返りません: %+v", payload)
	}
}

func TestHandlePipelineList(t *testing.T) {
	t.Parallel()

	stub := &stubEngine{
		pipelines: []engine.PipelineDef{{Type: "demo", Version: "v1"}},
	}
	mux := newTestMux(stub)
	req := httptest.NewRequest(http.MethodGet, "/v1/config/pipelines", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	assertStatus(t, resp.Code, http.StatusOK)
	var payload struct {
		Pipelines []engine.PipelineDef `json:"pipelines"`
	}
	decodeJSON(t, resp.Body.Bytes(), &payload)
	if len(payload.Pipelines) != 1 || payload.Pipelines[0].Type != "demo" {
		t.Fatalf("unexpected pipeline list: %+v", payload)
	}
}

func TestHandleMetrics(t *testing.T) {
	t.Parallel()

	metrics.ObserveProviderCall("openai", time.Millisecond, nil)
	mux := newTestMux(&stubEngine{})
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	assertStatus(t, resp.Code, http.StatusOK)
	var payload map[string]map[string]int64
	decodeJSON(t, resp.Body.Bytes(), &payload)
	if counts := payload["provider_call_count"]; counts == nil || counts["openai"] == 0 {
		t.Fatalf("metrics payload missing provider_call_count: %+v", payload)
	}
}

type stubEngine struct {
	runJobFunc        func(ctx context.Context, req engine.JobRequest) (*engine.Job, error)
	runJobStreamFunc  func(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error)
	cancelJobFunc     func(ctx context.Context, jobID string, reason string) error
	getJobFunc        func(ctx context.Context, jobID string) (*engine.Job, error)
	upsertProfileFunc func(engine.ProviderProfile) error
	pipelines         []engine.PipelineDef
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

func (s *stubEngine) UpsertProviderProfile(profile engine.ProviderProfile) error {
	if s.upsertProfileFunc == nil {
		return errors.New("upsert not implemented")
	}
	return s.upsertProfileFunc(profile)
}

func (s *stubEngine) ListPipelines() []engine.PipelineDef {
	return s.pipelines
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

func newTestMux(e engine.Engine) *http.ServeMux {
	mux := http.NewServeMux()
	handler := server.NewHandler(e, time.Unix(0, 0), "test-version")
	handler.Register(mux)
	return mux
}

func assertStatus(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("HTTP ステータスが想定外です: got=%d want=%d", got, want)
	}
}

func decodeJSON(t *testing.T, data []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("JSON 解析に失敗しました: %v", err)
	}
}
