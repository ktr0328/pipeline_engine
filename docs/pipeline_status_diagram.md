# Pipeline Status Flow

下図は `openai.funmarkdown.v1` のような 3 ステップ構成パイプラインが実行される際のステータス変化を表しています。`Job` ステータスと各 `StepExecution` の遷移を対で示し、`stream=true` のストリーミングイベントがどのタイミングで発生するかもメモしています。

```mermaid
stateDiagram-v2
    [*] --> JobQueued
    JobQueued: job.status = queued
    JobQueued --> JobRunning: RunJob
    JobRunning: job.status = running
    JobRunning --> JobSucceeded
    JobSucceeded: job.status = succeeded
    JobRunning --> JobFailed
    JobRunning --> JobCancelled

    state JobRunning {
        [*] --> StepTrivia
        StepTrivia: step "trivia"
        StepTrivia --> StepEnrich
        StepEnrich: step "enrich"
        StepEnrich --> StepMarkdown
        StepMarkdown: step "markdown"
        StepMarkdown --> [*]
    }

    note right of JobQueued: job_queued
    note right of JobRunning: job_started / job_status
    note right of StepTrivia: step_started / step_completed / item_completed
    note right of StepEnrich: step_started / step_completed / item_completed
    note right of StepMarkdown: step_started / step_completed / item_completed
    note right of JobSucceeded: job_status(succeeded) / job_completed / stream_finished
```

- `JobQueued` → `JobRunning`：`RunJob` が呼ばれ、`job_started` と最新 `job_status` が送出されます。
- 各ステップ（trivia → enrich → markdown）は `step_started` → `step_completed` を発火し、`Export=true` のため `item_completed` で途中結果を配信します。
- 全ステップが `success` になると `job_status`(succeeded) と `job_completed`、そして終端を示す `stream_finished` が送出されます。
- エラー / キャンセル時は `job_failed` / `job_cancelled` とともに `stream_finished` が届き、該当ステップにも `step_failed` / `step_cancelled` が送出されます。
