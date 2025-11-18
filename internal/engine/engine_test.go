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

	req := engine.JobRequest{
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

	ctx := context.Background()
	job, err := eng.RunJob(ctx, req)
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
