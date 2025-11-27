package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/store"
	"github.com/example/pipeline-engine/pkg/logging"
)

// Handler wires HTTP requests to the engine implementation.
type Handler struct {
	engine    engine.Engine
	startedAt time.Time
	version   string
	eventMu   sync.RWMutex
	eventSeq  map[string]uint64
	eventLogs map[string][]engine.StreamingEvent
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

type providerProfileRequest struct {
	ID           engine.ProviderProfileID `json:"id"`
	Kind         engine.ProviderKind      `json:"kind"`
	BaseURI      string                   `json:"base_uri"`
	APIKey       string                   `json:"api_key"`
	DefaultModel string                   `json:"default_model"`
	Extra        map[string]any           `json:"extra"`
}

type engineConfigRequest struct {
	LogLevel string `json:"log_level"`
}

// NewHandler creates a Handler.
func NewHandler(e engine.Engine, startedAt time.Time, version string) *Handler {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	if version == "" {
		version = Version
	}
	return &Handler{
		engine:    e,
		startedAt: startedAt,
		version:   version,
		eventSeq:  map[string]uint64{},
		eventLogs: map[string][]engine.StreamingEvent{},
	}
}

// Register registers all HTTP routes.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/v1/jobs", h.handleJobs)
	mux.HandleFunc("/v1/jobs/", h.handleJobOps)
	mux.HandleFunc("/v1/config/providers", h.handleProviderConfig)
	mux.HandleFunc("/v1/config/engine", h.handleEngineConfig)
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
		writeMethodNotAllowed(w)
	}
}

func (h *Handler) handleJobOps(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeNotFound(w)
		return
	}

	jobID := parts[0]

	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			h.getJob(w, r, jobID)
			return
		}
		writeMethodNotAllowed(w)
		return
	}

	action := parts[1]
	switch action {
	case "stream":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		h.streamExistingJob(w, r, jobID)
	case "cancel":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		h.cancelJob(w, r, jobID)
	case "rerun":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		h.rerunJob(w, r, jobID)
	default:
		writeNotFound(w)
	}
}

func (h *Handler) handleProviderConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	defer r.Body.Close()
	var payload providerProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid payload: %v", err), nil)
		return
	}
	if payload.ID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "id is required", nil)
		return
	}
	profile := engine.ProviderProfile{
		ID:           payload.ID,
		Kind:         payload.Kind,
		BaseURI:      payload.BaseURI,
		APIKey:       payload.APIKey,
		DefaultModel: payload.DefaultModel,
		Extra:        payload.Extra,
	}
	if err := h.engine.UpsertProviderProfile(profile); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "config_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (h *Handler) handleEngineConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	defer r.Body.Close()
	var payload engineConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid payload: %v", err), nil)
		return
	}
	resp := map[string]any{}
	if payload.LogLevel != "" {
		level := logging.SetLevelFromString(payload.LogLevel)
		resp["log_level"] = level.String()
	}
	if len(resp) == 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "no configuration provided", nil)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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

		queued := h.appendEvent(engine.StreamingEvent{Event: "job_queued", JobID: job.ID, Data: job})
		if err := enc.Encode(queued); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}

		for event := range events {
			event = h.appendEvent(event)
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

	var afterSeq uint64
	if raw := r.URL.Query().Get("after_seq"); raw != "" {
		if val, err := strconv.ParseUint(raw, 10, 64); err == nil {
			afterSeq = val
		}
	}

	ctx := r.Context()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	tracker := engine.NewStreamingTracker()
	lastSeq := afterSeq

	for {
		sent := false
		if events := h.eventsAfter(jobID, lastSeq); len(events) > 0 {
			for _, event := range events {
				if event.Seq <= lastSeq {
					continue
				}
				if err := enc.Encode(event); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
				lastSeq = event.Seq
				sent = true
				if event.Event == "stream_finished" {
					return
				}
			}
		} else if !h.hasEventLog(jobID) {
			job, err := h.engine.GetJob(ctx, jobID)
			if err != nil {
				h.writeStreamError(enc, flusher, jobID, err)
				return
			}

			for _, event := range tracker.Diff(job) {
				event = h.appendEvent(event)
				if event.Seq <= lastSeq {
					continue
				}
				if err := enc.Encode(event); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
				lastSeq = event.Seq
				sent = true
				if event.Event == "stream_finished" {
					return
				}
			}

			if isTerminal(job.Status) && !sent {
				return
			}
		}

		if sent {
			continue
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

func (h *Handler) writeStreamError(enc *json.Encoder, flusher http.Flusher, jobID string, err error) {
	evt := h.appendEvent(engine.StreamingEvent{Event: "error", JobID: jobID, Data: err.Error()})
	_ = enc.Encode(evt)
	if flusher != nil {
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeNotFound(w http.ResponseWriter) {
	writeAPIError(w, http.StatusNotFound, "not_found", "resource not found", nil)
}

func (h *Handler) appendEvent(evt engine.StreamingEvent) engine.StreamingEvent {
	if evt.JobID == "" {
		return evt
	}
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	seq := h.eventSeq[evt.JobID] + 1
	evt.Seq = seq
	h.eventSeq[evt.JobID] = seq
	h.eventLogs[evt.JobID] = append(h.eventLogs[evt.JobID], evt)
	return evt
}

func (h *Handler) eventsAfter(jobID string, afterSeq uint64) []engine.StreamingEvent {
	h.eventMu.RLock()
	defer h.eventMu.RUnlock()
	events := h.eventLogs[jobID]
	if len(events) == 0 {
		return nil
	}
	result := make([]engine.StreamingEvent, 0, len(events))
	for _, evt := range events {
		if evt.Seq > afterSeq {
			result = append(result, evt)
		}
	}
	return result
}

func (h *Handler) hasEventLog(jobID string) bool {
	h.eventMu.RLock()
	defer h.eventMu.RUnlock()
	if seq, ok := h.eventSeq[jobID]; ok && seq > 0 {
		return true
	}
	if events := h.eventLogs[jobID]; len(events) > 0 {
		return true
	}
	return false
}

func (h *Handler) lastLoggedEvent(jobID string) *engine.StreamingEvent {
	h.eventMu.RLock()
	defer h.eventMu.RUnlock()
	events := h.eventLogs[jobID]
	if len(events) == 0 {
		return nil
	}
	evt := events[len(events)-1]
	return &evt
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
}
