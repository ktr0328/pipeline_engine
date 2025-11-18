package engine

import "testing"

func TestStreamingTrackerEmitsJobAndStepEvents(t *testing.T) {
	tracker := NewStreamingTracker()

	job := &Job{ID: "job-1", Status: JobStatusQueued, StepExecutions: []StepExecution{{StepID: StepID("step-1"), Status: StepExecPending}}}
	events := tracker.Diff(job)
	if len(events) == 0 || events[0].Event != "job_status" {
		t.Fatalf("初回状態で job_status イベントが発生しません: %+v", events)
	}

	job.Status = JobStatusRunning
	job.StepExecutions[0].Status = StepExecRunning
	events = tracker.Diff(job)
	if !containsEvent(events, "job_started") {
		t.Fatalf("job_started イベントが含まれていません: %+v", events)
	}
	if !containsEvent(events, "step_started") {
		t.Fatalf("step_started イベントが含まれていません: %+v", events)
	}

	job.StepExecutions[0].Status = StepExecSuccess
	job.Result = &JobResult{Items: []ResultItem{{ID: "item-1", Label: "result", StepID: StepID("step-1"), Kind: "text", ContentType: ContentText}}}
	job.Status = JobStatusSucceeded
	events = tracker.Diff(job)
	if !containsEvent(events, "step_completed") {
		t.Fatalf("step_completed イベントが含まれていません: %+v", events)
	}
	if !containsEvent(events, "item_completed") {
		t.Fatalf("item_completed イベントが含まれていません: %+v", events)
	}
	if !containsEvent(events, "job_completed") {
			 t.Fatalf("job_completed イベントが含まれていません: %+v", events)
	}
	if !containsEvent(events, "stream_finished") {
			 t.Fatalf("stream_finished イベントが含まれていません: %+v", events)
	}
}

func containsEvent(events []StreamingEvent, name string) bool {
	for _, evt := range events {
		if evt.Event == name {
			return true
		}
	}
	return false
}
