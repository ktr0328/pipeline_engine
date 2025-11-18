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

## ビルド

```
npm install
npm run build
```

`dist/` に ESM + 型定義が出力されます。必要に応じて `npm publish` の前に `package.json` の `name` や `version` を調整してください。
