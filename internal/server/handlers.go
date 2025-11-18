package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/store"
)

// Handler wires HTTP requests to the engine implementation.
type Handler struct {
	engine    engine.Engine
	startedAt time.Time
	version   string
}

type rerunRequest struct {
	FromStepID    *engine.StepID   `json:"from_step_id"`
	ReuseUpstream bool             `json:"reuse_upstream"`
	OverrideInput *engine.JobInput `json:"override_input"`
}

type jobResponse struct {
	Job *engine.Job `json:"job"`
}

type apiErrorPayload struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

type apiErrorResponse struct {
	Error apiErrorPayload `json:"error"`
}

// NewHandler creates a Handler.
func NewHandler(e engine.Engine, startedAt time.Time, version string) *Handler {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	if version == "" {
		version = Version
	}
	return &Handler{engine: e, startedAt: startedAt, version: version}
}

// Register registers all HTTP routes.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/v1/jobs", h.handleJobs)
	mux.HandleFunc("/v1/jobs/", h.handleJobOps)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	payload := map[string]interface{}{
		"status":     "ok",
		"version":    h.version,
		"uptime_sec": time.Since(h.startedAt).Seconds(),
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createJob(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleJobOps(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	jobID := parts[0]

	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			h.getJob(w, r, jobID)
			return
		}
		http.NotFound(w, r)
		return
	}

	action := parts[1]
	switch action {
	case "stream":
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		h.streamExistingJob(w, r, jobID)
	case "cancel":
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		h.cancelJob(w, r, jobID)
	case "rerun":
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		h.rerunJob(w, r, jobID)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) createJob(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req engine.JobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid payload: %v", err), nil)
		return
	}

	if r.URL.Query().Get("stream") == "true" {
		events, job, err := h.engine.RunJobStream(r.Context(), req)
		if err != nil {
			handleEngineError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		flusher, _ := w.(http.Flusher)

		queued := engine.StreamingEvent{Event: "job_queued", JobID: job.ID, Data: job}
		if err := enc.Encode(queued); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}

		for event := range events {
			if err := enc.Encode(event); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		return
	}

	job, err := h.engine.RunJob(r.Context(), req)
	if err != nil {
		handleEngineError(w, err)
		return
	}

	writeJobResponse(w, http.StatusAccepted, job)
}

func (h *Handler) getJob(w http.ResponseWriter, r *http.Request, jobID string) {
	job, err := h.engine.GetJob(r.Context(), jobID)
	if err != nil {
		handleEngineError(w, err)
		return
	}
	writeJobResponse(w, http.StatusOK, job)
}

func (h *Handler) cancelJob(w http.ResponseWriter, r *http.Request, jobID string) {
	defer r.Body.Close()
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid payload: %v", err), nil)
		return
	}

	if err := h.engine.CancelJob(r.Context(), jobID, payload.Reason); err != nil {
		handleEngineError(w, err)
		return
	}

	job, err := h.engine.GetJob(r.Context(), jobID)
	if err != nil {
		handleEngineError(w, err)
		return
	}

	writeJobResponse(w, http.StatusOK, job)
}

func (h *Handler) rerunJob(w http.ResponseWriter, r *http.Request, jobID string) {
	defer r.Body.Close()
	var payload rerunRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid payload: %v", err), nil)
		return
	}

	baseJob, err := h.engine.GetJob(r.Context(), jobID)
	if err != nil {
		handleEngineError(w, err)
		return
	}

	nextInput := baseJob.Input
	if payload.OverrideInput != nil {
		nextInput = *payload.OverrideInput
	}

	var parentID *string
	if baseJob.ID != "" {
		id := baseJob.ID
		parentID = &id
	}

	var fromStep *engine.StepID
	if payload.FromStepID != nil {
		step := *payload.FromStepID
		fromStep = &step
	}

	req := engine.JobRequest{
		PipelineType:  baseJob.PipelineType,
		Input:         nextInput,
		Mode:          "rerun",
		ParentJobID:   parentID,
		FromStepID:    fromStep,
		ReuseUpstream: payload.ReuseUpstream,
	}

	job, err := h.engine.RunJob(r.Context(), req)
	if err != nil {
		handleEngineError(w, err)
		return
	}

	writeJobResponse(w, http.StatusAccepted, job)
}

func (h *Handler) streamExistingJob(w http.ResponseWriter, r *http.Request, jobID string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)

	ctx := r.Context()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	tracker := engine.NewStreamingTracker()
	for {
		job, err := h.engine.GetJob(ctx, jobID)
		if err != nil {
			writeStreamError(enc, flusher, jobID, err)
			return
		}

		for _, event := range tracker.Diff(job) {
			if err := enc.Encode(event); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}

		if isTerminal(job.Status) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func isTerminal(status engine.JobStatus) bool {
	switch status {
	case engine.JobStatusSucceeded, engine.JobStatusFailed, engine.JobStatusCancelled:
		return true
	default:
		return false
	}
}

func handleEngineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrJobNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
	}
}

func writeJobResponse(w http.ResponseWriter, status int, job *engine.Job) {
	writeJSON(w, status, jobResponse{Job: job})
}

func writeAPIError(w http.ResponseWriter, status int, code, message string, details interface{}) {
	payload := apiErrorResponse{Error: apiErrorPayload{Code: code, Message: message, Details: details}}
	writeJSON(w, status, payload)
}

func writeStreamError(enc *json.Encoder, flusher http.Flusher, jobID string, err error) {
	_ = enc.Encode(engine.StreamingEvent{Event: "error", JobID: jobID, Data: err.Error()})
	if flusher != nil {
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
