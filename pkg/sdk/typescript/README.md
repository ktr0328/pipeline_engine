# Pipeline Engine TypeScript SDK

Node.js / Electron アプリから Pipeline Engine の HTTP API を扱うための軽量クライアントです。現状は以下をサポートしています。

- ジョブの作成・取得・キャンセル・リラン
- Chunk 付きストリーミング (`provider_chunk` / `item_completed` / `stream_finished` を逐次取得)

## インストール

```
npm install @pipeforge/sdk
```

※この SDK は Node.js 18 以降（組み込みの `fetch` / WHATWG Streams 利用）を前提にしています。Electron ではメインプロセスで利用するか、レンダラから `fetch` を注入してください。

## 使い方

```ts
import { PipelineEngineClient, JobRequest } from "@pipeforge/sdk";

const client = new PipelineEngineClient({ baseUrl: "http://127.0.0.1:8085" });

const jobReq: JobRequest = {
  pipeline_type: "openai.funmarkdown.v1",
  input: {
    sources: [{ kind: "note", content: "雑学を話して" }]
  }
};

const { job, events } = await client.streamJobs(jobReq);
console.log("job accepted", job.id);

for await (const evt of events) {
  switch (evt.event) {
    case "provider_chunk":
      console.log("chunk", evt.data.content);
      break;
    case "item_completed":
      console.log("step completed", evt.data.step_id);
      break;
    case "stream_finished":
      console.log("done");
      break;
  }
}
```

### グローバル設定（Provider Profile）の更新

```ts
await client.upsertProviderProfile({
  id: "openai-cli",
  kind: "openai",
  api_key: process.env.OPENAI_API_KEY,
  base_uri: "https://api.openai.com/v1",
  default_model: "gpt-4o-mini"
});

await client.updateEngineConfig({ log_level: "debug" });
```
```

`client.streamJobs()` は `AsyncIterable<StreamingEvent>` を返すので、Electron では chunk が到着するたびに UI を更新できます。

### ジョブストリームの再開 (`after_seq`)

`streamJobByID(jobID, afterSeq)` を使うと途中で切れたストリームを `seq` 番号から再開できます。最後に受け取った `StreamingEvent.seq` を記録し、次回リクエストで `afterSeq` に渡してください。

```ts
let resumeSeq = 0;

async function consume(jobID: string) {
  const stream = await client.streamJobByID(jobID, resumeSeq);
  for await (const evt of stream) {
    resumeSeq = evt.seq ?? resumeSeq;
    renderEvent(evt);
  }
}
```

`after_seq` がサーバー側の履歴とずれた場合は、フルストリーム（`after_seq=0`）で取り直してください。詳しい仕様は [docs/mcp/StreamingResume.md](../../docs/mcp/StreamingResume.md) を参照してください。

### パイプライン一覧・メトリクス取得

```ts
const pipelines = await client.listPipelines();
const metrics = await client.getMetrics();
console.log(pipelines.map((p) => p.type));
console.log(metrics.provider_call_count);
```

`listPipelines()` は `/v1/config/pipelines` のラッパーで登録済みの PipelineDef を返し、`getMetrics()` は `/v1/metrics` の `provider_call_count` / `provider_call_latency` などをまとめて返します。

## MCP Adapter CLI (`pipeline-engine-mcp`)

`@pipeforge/sdk` には MCP (Model Context Protocol) アダプタ CLI が同梱されています。Node.js 18+ 環境で `npm install -g @pipeforge/sdk` を実行すると `pipeline-engine-mcp` コマンドが利用できます。

```bash
npm install -g @pipeforge/sdk
PIPELINE_ENGINE_ADDR="http://127.0.0.1:8085" pipeline-engine-mcp
```

manifest 例（Claude Desktop / Cursor 用）:

```jsonc
{
  "name": "pipeforge",
  "description": "Pipeline Engine MCP adapter",
  "commands": [
    {
      "command": "pipeline-engine-mcp",
      "env": {
        "PIPELINE_ENGINE_ADDR": "http://127.0.0.1:8085",
        "PIPELINE_ENGINE_OPENAI_API_KEY": "${PIPELINE_ENGINE_OPENAI_API_KEY}"
      }
    }
  ],
  "tools": [
    { "name": "startPipeline", "description": "Create job via /v1/jobs" },
    { "name": "streamJob", "description": "Forward NDJSON events" },
    { "name": "getJob", "description": "Fetch job result" },
    { "name": "cancelJob", "description": "Cancel running job" },
    { "name": "rerunJob", "description": "Rerun job from step" },
    { "name": "upsertProviderProfile", "description": "Update provider profile" }
  ]
}
```

`streamJob` / `startPipeline(stream=true)` では `tool_event` が逐次送信されます。`params.kind`（`status`/`chunk`/`result`/`error`）と `params.seq` を利用すると、クライアント側でトークン描画や欠落検知が容易になります。manifest の詳細や MCP ホストへの登録手順は [docs/mcp/ManifestGuide.md](../../docs/mcp/ManifestGuide.md) を参照してください。

## ビルド

```
npm install
npm run build
```

`dist/` に ESM + 型定義が出力されます。必要に応じて `npm publish` の前に `package.json` の `name` や `version` を調整してください。
