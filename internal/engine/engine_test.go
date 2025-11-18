package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	"github.com/example/pipeline-engine/internal/store"
)

func TestBasicEngine_RunJobWithSamplePipeline(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	eng := engine.NewBasicEngine(memoryStore)

	ctx := context.Background()
	job, err := eng.RunJob(ctx, sampleJobRequest())
	if err != nil {
		t.Fatalf("ジョブの起動に失敗しました: %v", err)
	}

	finalJob := waitForJobStatus(t, memoryStore, job.ID, engine.JobStatusSucceeded, 3*time.Second)

	if finalJob.Result == nil {
		t.Fatalf("ジョブ %s の結果が nil です", finalJob.ID)
	}

	if len(finalJob.Result.Items) == 0 {
		t.Fatalf("ジョブ %s の結果アイテムが空です", finalJob.ID)
	}

	item := finalJob.Result.Items[0]
	if item.Kind != "text" {
		t.Fatalf("結果アイテムの Kind が想定外です: %s", item.Kind)
	}
	if item.ContentType != engine.ContentText {
		t.Fatalf("結果アイテムの ContentType が text ではありません: %s", item.ContentType)
	}

	data, ok := item.Data.(map[string]any)
	if !ok {
		t.Fatalf("結果アイテムの Data を map に変換できませんでした: %#v", item.Data)
	}

	text, ok := data["text"].(string)
	if !ok || text == "" {
		t.Fatalf("結果アイテムの text フィールドが空、または文字列ではありません: %#v", data["text"])
	}

	if len(finalJob.StepExecutions) != 1 {
		t.Fatalf("ステップ実行数が想定外です: %+v", finalJob.StepExecutions)
	}

	if finalJob.StepExecutions[0].Status != engine.StepExecSuccess {
		t.Fatalf("最初のステップの状態が success ではありません: %s", finalJob.StepExecutions[0].Status)
	}
}

func TestBasicEngine_CancelJobStopsExecution(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	eng := engine.NewBasicEngine(memoryStore)

	ctx := context.Background()
	job, err := eng.RunJob(ctx, sampleJobRequest())
	if err != nil {
		t.Fatalf("ジョブの起動に失敗しました: %v", err)
	}

	_ = waitForJobStatus(t, memoryStore, job.ID, engine.JobStatusRunning, 2*time.Second)

	if err := eng.CancelJob(ctx, job.ID, "test cancel"); err != nil {
		t.Fatalf("ジョブのキャンセルに失敗しました: %v", err)
	}

	finalJob := waitForJobStatus(t, memoryStore, job.ID, engine.JobStatusCancelled, 3*time.Second)
	if finalJob.Error == nil || finalJob.Error.Code != "cancelled" {
		t.Fatalf("キャンセル後のエラー情報が不正です: %+v", finalJob.Error)
	}

	for _, step := range finalJob.StepExecutions {
		if step.Status != engine.StepExecCancelled {
			t.Fatalf("ステップ %s が cancelled ではありません: %s", step.StepID, step.Status)
		}
	}
}

func TestBasicEngine_RunJobStreamEmitsStatusTransitions(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	eng := engine.NewBasicEngine(memoryStore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, job, err := eng.RunJobStream(ctx, sampleJobRequest())
	if err != nil {
		t.Fatalf("ジョブストリームの起動に失敗しました: %v", err)
	}
	if job == nil {
		t.Fatal("RunJobStream から返却されたジョブが nil です")
	}

	statuses := make([]engine.JobStatus, 0, 3)
	timeout := time.After(3 * time.Second)

collectLoop:
	for {
		select {
		case <-timeout:
			t.Fatal("ストリーミングイベントの待機がタイムアウトしました")
		case ev, ok := <-events:
			if !ok {
				break collectLoop
			}
			if ev.Event != "job_status" {
				continue
			}
			jobData, ok := ev.Data.(*engine.Job)
			if !ok {
				t.Fatalf("event data が *engine.Job ではありません: %T", ev.Data)
			}
			statuses = append(statuses, jobData.Status)
			if jobData.Status == engine.JobStatusSucceeded {
				break collectLoop
			}
		}
	}

	if len(statuses) == 0 {
		t.Fatal("job_status イベントが受信できませんでした")
	}

	last := statuses[len(statuses)-1]
	if last != engine.JobStatusSucceeded {
		t.Fatalf("最終ステータスが succeeded ではありません: %s (取得済み: %v)", last, statuses)
	}
}

func waitForJobStatus(t *testing.T, jobStore engine.JobStore, jobID string, expected engine.JobStatus, timeout time.Duration) *engine.Job {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := jobStore.GetJob(jobID)
		if err != nil {
			t.Fatalf("ジョブ %s の取得に失敗しました: %v", jobID, err)
		}

		if job.Status == expected {
			return job
		}

		if job.Status == engine.JobStatusFailed || job.Status == engine.JobStatusCancelled {
			t.Fatalf("ジョブ %s が予期せぬ最終状態になりました: %s", jobID, job.Status)
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("ジョブ %s が制限時間内に %s になりませんでした", jobID, expected)
	return nil
}

func sampleJobRequest() engine.JobRequest {
	return engine.JobRequest{
		PipelineType: engine.PipelineType("sample_pipeline"),
		Input: engine.JobInput{
			Sources: []engine.Source{
				{
					Kind:    engine.SourceKindNote,
					Label:   "仕様メモ",
					Content: "このサンプルはパイプラインの疎通確認用です。",
				},
			},
			Options: &engine.JobOptions{
				Language: "ja",
			},
		},
	}
}
