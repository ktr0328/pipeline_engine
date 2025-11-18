# Streaming Events Schema

`POST /v1/jobs?stream=true` もしくは `GET /v1/jobs/{id}/stream` では 1 行 1 JSON の NDJSON を返します。各イベントは以下のフィールドを持ちます。

```json
{
  "event": "step_started",
  "job_id": "<job id>",
  "data": { ... }
}
```

## 主な event 種別
- `job_status`, `job_started`, `job_completed`, `job_failed`, `job_cancelled`, `stream_finished`
- `step_started`, `step_completed`, `step_failed`, `step_cancelled`
- `item_completed` – `data` には `ResultItem`
- `provider_chunk` – `data` は `StepChunk` で `{ "step_id": "...", "index": 0, "content": "部分テキスト" }`
- `error` – 文字列メッセージ

## StepChunk / ResultItem
- `StepChunk`: StepExecution に随時蓄積される chunk。`index` は 0 始まり。
- `ResultItem`: `kind`, `content_type`, `data`（`text`, `prompt`, `pipelineType` 等）を含む。

### JSON Schema（抜粋）

```json
{
  "type": "object",
  "required": ["event", "job_id", "data"],
  "properties": {
    "event": {"type": "string"},
    "job_id": {"type": "string"},
    "data": {}
  }
}
```

`provider_chunk` 例:

```json
{
  "event": "provider_chunk",
  "job_id": "job-123",
  "data": {"step_id": "markdown", "index": 1, "content": "続き…"}
}
```

ストリームは `stream_finished` でクローズを明示するため、クライアントはこのイベントを受信して処理を終了してください。失敗 (`job_failed`) やキャンセル (`job_cancelled`) の場合も `stream_finished` が送られます。
