package engine

// StreamingTracker tracks job state to emit incremental StreamingEvent values.
type StreamingTracker struct {
	lastStatus    JobStatus
	stepStatus    map[StepID]StepExecutionStatus
	lastItemCount int
	sentStarted   bool
	chunkCount    map[StepID]int
}

// NewStreamingTracker returns an initialized tracker.
func NewStreamingTracker() *StreamingTracker {
	return &StreamingTracker{stepStatus: map[StepID]StepExecutionStatus{}, chunkCount: map[StepID]int{}}
}

// Diff compares the provided job against prior state and returns events to emit.
func (t *StreamingTracker) Diff(job *Job) []StreamingEvent {
	if job == nil {
		return nil
	}
	events := make([]StreamingEvent, 0, 4)

	if job.Status != t.lastStatus {
		if job.Status == JobStatusRunning && !t.sentStarted {
			events = append(events, StreamingEvent{Event: "job_started", JobID: job.ID, Data: job})
			t.sentStarted = true
		}
		t.lastStatus = job.Status
		events = append(events, StreamingEvent{Event: "job_status", JobID: job.ID, Data: job})
		if isTerminal(job.Status) {
			name := "job_completed"
			switch job.Status {
			case JobStatusFailed:
				name = "job_failed"
			case JobStatusCancelled:
				name = "job_cancelled"
			}
			events = append(events, StreamingEvent{Event: name, JobID: job.ID, Data: job})
			events = append(events, StreamingEvent{Event: "stream_finished", JobID: job.ID, Data: job})
		}
	}

	for _, step := range job.StepExecutions {
		prev := t.stepStatus[step.StepID]
		if step.Status != prev {
			t.stepStatus[step.StepID] = step.Status
			switch step.Status {
			case StepExecRunning:
				events = append(events, StreamingEvent{Event: "step_started", JobID: job.ID, Data: step})
			case StepExecSuccess:
				events = append(events, StreamingEvent{Event: "step_completed", JobID: job.ID, Data: step})
			case StepExecFailed:
				events = append(events, StreamingEvent{Event: "step_failed", JobID: job.ID, Data: step})
			case StepExecCancelled:
				events = append(events, StreamingEvent{Event: "step_cancelled", JobID: job.ID, Data: step})
			}
		}

		if len(step.Chunks) > 0 {
			seen := t.chunkCount[step.StepID]
			if len(step.Chunks) > seen {
				for _, chunk := range step.Chunks[seen:] {
					events = append(events, StreamingEvent{Event: "provider_chunk", JobID: job.ID, Data: chunk})
				}
				t.chunkCount[step.StepID] = len(step.Chunks)
			}
		}
	}

	itemCount := 0
	if job.Result != nil {
		itemCount = len(job.Result.Items)
	}
	if job.Result != nil && itemCount > t.lastItemCount {
		for i := t.lastItemCount; i < len(job.Result.Items); i++ {
			events = append(events, StreamingEvent{Event: "item_completed", JobID: job.ID, Data: job.Result.Items[i]})
		}
	}
	t.lastItemCount = itemCount

	return events
}
