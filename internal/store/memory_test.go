package store_test

import (
	"testing"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/store"
)

func TestMemoryStore_CreateAndGetJob(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	job := newTestJob("job-create")

	if err := memoryStore.CreateJob(job); err != nil {
		t.Fatalf("CreateJob に失敗しました: %v", err)
	}

	retrieved, err := memoryStore.GetJob(job.ID)
	if err != nil {
		t.Fatalf("保存済みジョブの取得に失敗しました: %v", err)
	}

	if retrieved == job {
		t.Fatal("保存したポインタがそのまま返却されています")
	}

	if retrieved.Status != job.Status {
		t.Fatalf("ジョブのステータスが一致しません: %s vs %s", retrieved.Status, job.Status)
	}

	retrieved.StepExecutions[0].Status = engine.StepExecFailed
	retrieved.Result.Items[0].Label = "changed"

	reloaded, err := memoryStore.GetJob(job.ID)
	if err != nil {
		t.Fatalf("ジョブの再取得に失敗しました: %v", err)
	}

	if reloaded.StepExecutions[0].Status == engine.StepExecFailed {
		t.Fatal("StepExecutions がコピーされておらず共有参照になっています")
	}
	if reloaded.Result.Items[0].Label == "changed" {
		t.Fatal("Result.Items がコピーされておらず共有参照になっています")
	}
}

func TestMemoryStore_UpdateJob(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	job := newTestJob("job-update")
	if err := memoryStore.CreateJob(job); err != nil {
		t.Fatalf("CreateJob に失敗しました: %v", err)
	}

	job.Status = engine.JobStatusRunning
	job.StepExecutions[0].Status = engine.StepExecRunning
	if err := memoryStore.UpdateJob(job); err != nil {
		t.Fatalf("UpdateJob に失敗しました: %v", err)
	}

	updated, err := memoryStore.GetJob(job.ID)
	if err != nil {
		t.Fatalf("Update 後の取得に失敗しました: %v", err)
	}

	if updated.Status != engine.JobStatusRunning {
		t.Fatalf("ジョブステータスが更新されていません: %s", updated.Status)
	}
	if updated.StepExecutions[0].Status != engine.StepExecRunning {
		t.Fatalf("ステップステータスが更新されていません: %s", updated.StepExecutions[0].Status)
	}
}

func TestMemoryStore_ListJobsReturnsCopies(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	jobA := newTestJob("job-a")
	jobB := newTestJob("job-b")

	if err := memoryStore.CreateJob(jobA); err != nil {
		t.Fatalf("jobA の作成に失敗しました: %v", err)
	}
	if err := memoryStore.CreateJob(jobB); err != nil {
		t.Fatalf("jobB の作成に失敗しました: %v", err)
	}

	jobs, err := memoryStore.ListJobs()
	if err != nil {
		t.Fatalf("ListJobs の実行に失敗しました: %v", err)
	}

	if len(jobs) != 2 {
		t.Fatalf("ジョブ数が想定外です: %d", len(jobs))
	}

	for _, j := range jobs {
		j.Status = engine.JobStatusFailed
	}

	reloadedA, err := memoryStore.GetJob(jobA.ID)
	if err != nil {
		t.Fatalf("jobA の再取得に失敗しました: %v", err)
	}

	if reloadedA.Status == engine.JobStatusFailed {
		t.Fatal("ListJobs の戻り値を変更するとストア内のデータが変化しています")
	}
}

func TestMemoryStore_Checkpoints(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	items := []engine.ResultItem{
		{ID: "item-1", Label: "summary", StepID: engine.StepID("step-1"), Kind: "text", ContentType: engine.ContentText, Data: map[string]any{"text": "dummy"}},
	}

	memoryStore.SaveCheckpoint("job-1", engine.StepID("step-1"), items)
	loaded := memoryStore.LoadCheckpoints("job-1")
	if len(loaded) != 1 {
		mem := loaded
		t.Fatalf("checkpoint が保存されていません: %+v", mem)
	}
	if _, ok := loaded[engine.StepID("step-1")]; !ok {
		t.Fatalf("step-1 checkpoint が見つかりません: %+v", loaded)
	}

	// ensure deep copy
	items[0].Label = "mutated"
	if loaded[engine.StepID("step-1")][0].Label == "mutated" {
		t.Fatalf("checkpoint がディープコピーされていません")
	}

	memoryStore.ClearCheckpoints("job-1")
	if cp := memoryStore.LoadCheckpoints("job-1"); cp != nil {
		t.Fatalf("ClearCheckpoints 後もデータが残っています: %+v", cp)
	}
}

func newTestJob(id string) *engine.Job {
	now := time.Now().UTC()
	return &engine.Job{
		ID:              id,
		PipelineType:    engine.PipelineType("test"),
		PipelineVersion: "v0",
		Status:          engine.JobStatusQueued,
		CreatedAt:       now,
		UpdatedAt:       now,
		Input: engine.JobInput{
			Sources: []engine.Source{
				{Kind: engine.SourceKindNote, Label: "memo", Content: "test"},
			},
		},
		Result: &engine.JobResult{
			Items: []engine.ResultItem{
				{ID: "item-1", Label: "summary", StepID: engine.StepID("step-1"), Kind: "text", ContentType: engine.ContentText, Data: map[string]any{"text": "dummy"}},
			},
		},
		StepExecutions: []engine.StepExecution{
			{StepID: engine.StepID("step-1"), Status: engine.StepExecPending, StartedAt: &now},
		},
	}
}
