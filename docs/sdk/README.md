# SDK 設計メモ

Pipeline Engine は HTTP/NDJSON ベースの API を提供するため、SDK は `/health`, `/v1/jobs`, `/v1/jobs/{id}`, `/v1/jobs/{id}/stream`, `/v1/jobs/{id}/cancel`, `/v1/jobs/{id}/rerun` を薄く包む形で実装する。

## TypeScript / Node SDK

### 目標
- `createJob`, `createJobStream`, `getJob`, `cancelJob`, `rerunJob` など Promise ベースのラッパーを提供する。
- `EventEmitter` を用いて型付きイベントを発火する `JobStream` クラスを用意する（NDJSON をそのままイベントへマッピング）。
- Node（Fetch）だけでなく Browser/Electron でも使えるよう、`baseURL` や `fetch` 実装を差し替え可能にする。

### 構成案
```
src/
  client.ts       // PipelineEngineClient クラス（HTTP 操作用）
  stream.ts       // JobStream ヘルパー（EventSource / fetch streaming 対応）
  types.ts        // StepDef や Job などドメイン型の TypeScript 定義
```

### 利用イメージ
```ts
const client = new PipelineEngineClient({ baseURL: 'http://127.0.0.1:8085' });
const job = await client.createJob({ pipeline_type: 'summarize.v0', input: {...} });
const stream = await client.createJobStream({ pipeline_type: 'summarize.v0', input: {...} });
stream.on('item_completed', evt => console.log(evt.data));
await stream.untilDone();
```

### 実装メモ
- `fetch`（ブラウザ）や `node-fetch` を利用し、ヘッダー上書き（API キーや Host）も指定可能にする。
- ストリーミング処理はブラウザなら `ReadableStream`, Node なら `node:stream` で NDJSON を読み取る。
- npm パッケージ（`@pipeforge/sdk`）として公開し、型定義を同梱する。
- Electron でエンジンバイナリを子プロセスとして扱うための補助パッケージ `@pipeforge/engine` を別途用意し、`EngineProcess` でバイナリ起動と `/health` 待機をカプセル化する。`postinstall` は GitHub Releases のバイナリを既定で取得し、必要に応じて環境変数で URL / テンプレートを上書きできるようにする。

## Python SDK

### 目標
- 同期（requests）/ 非同期（httpx）クライアントの両方を用意する。
- `for event in client.stream_job(req): ...` 形式で使えるストリーミングジェネレーターを提供する。

### 構成案
```
pipeline_sdk/
  __init__.py
  client.py        # PipelineEngineClient（同期）
  aio.py           # AsyncPipelineEngineClient（httpx.AsyncClient）
  models.py        # Job / JobRequest / StreamingEvent の dataclass or Pydantic モデル
```

### 利用イメージ
```python
client = PipelineEngineClient(base_url="http://127.0.0.1:8085")
job = client.create_job(JobRequest(...))
for event in client.stream_job(JobRequest(...)):
    print(event.event, event.data)
```

### 実装メモ
- 型安全性のため `pydantic` or `dataclasses` を活用する。
- タイムアウトやリトライのフックを提供する。
- PyPI パッケージ（`pipeline-engine-sdk`）として公開する。

## 共通検討事項
- 将来的に ProviderProfile を読み込むヘルパー（設定ファイルからロードなど）を両 SDK に実装する。
- CLI 用途のスニペットや README から本ドキュメントへのリンクを整備し、開発者ガイドとして活用する。
