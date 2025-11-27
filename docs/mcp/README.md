# MCP Integration Design

`pipeline-engine` を Multimodal Connector Protocol (MCP) で操作できるようにするための設計メモです。目的は IDE / MCP Host からジョブ作成・ストリーミング取得・キャンセルなどを一貫して扱える薄いアダプタを提供することにあります。

## 1. コンポーネント構成
- **Pipeline Engine**: 既存の HTTP サーバー。`/v1/jobs`, `/v1/jobs/{id}/stream`, `/v1/config/providers` などの API を提供。
- **MCP Adapter**: `cmd/mcp-adapter`（Go）もしくは `pkg/sdk/typescript` ベースの CLI。MCP Host から stdio で呼ばれ、Tool Request を HTTP API にフォワードする。
- **MCP Host**: Claude Desktop, Cursor, VS Code MCP プラグインなど。`pipeforge.mcp.json` Manifest 経由で Adapter を登録する。

```
MCP Host ──(stdio)── MCP Adapter ──(HTTP/JSON)── Pipeline Engine
```

## 2. MCP Adapter の役割
1. MCP protocol (JSON-RPC over stdio) を処理し、Tool Request/Response を解釈。
2. Tool に応じて `pipeline-engine` の HTTP API を呼び、レスポンスを MCP の `result` に詰め直す。
3. `streamJob` では NDJSON ストリームを受け取り、MCP の Progress/Event（例: `event:provider_chunk`）として逐次配信。
4. Adapter 自身は状態レス維持を最小化し、必要に応じて `PIPELINE_ENGINE_ADDR`, `PIPELINE_ENGINE_API_TOKEN` などを環境変数から取得。

## 3. 提供する MCP ツール
| Tool 名 | 説明 | バッキング API | 追加メモ |
| ------- | ---- | --------------- | -------- |
| `startPipeline` | `pipeline_type`, `input`, `mode`, `stream` などを受け、新規 Job を作成 | `POST /v1/jobs` | レスポンスは `job_id`, `status`, `created_at` を返す |
| `streamJob` | NDJSON ストリームをリアルタイム転送 | `POST /v1/jobs?stream=true` or `GET /v1/jobs/{id}/stream` | MCP `event` に `job_status`, `provider_chunk`, `item_completed` 等をマップ |
| `getJob` | Job の詳細と結果を取得 | `GET /v1/jobs/{id}` | IDE が履歴を表示する用途 |
| `cancelJob` | 実行中ジョブをキャンセル | `POST /v1/jobs/{id}/cancel` | `reason` を引数で受け取れるようにする |
| `rerunJob` | `from_step_id`/`reuse_upstream` 付きのリラン | `POST /v1/jobs/{id}/rerun` | 戻り値は新しい Job ID |
| `upsertProviderProfile` | ProviderProfile を upsert | `POST /v1/config/providers` | API キーや Model を MCP 側で差し替え |
| `listPipelines` (拡張) | パイプライン定義一覧の取得 | `GET /v1/config/pipelines` (将来) or 手動登録 | IDEで候補を提示するための補助 |
| `listMetrics` (拡張) | `expvar` の主要メトリクスを取得 | `GET /debug/vars` | Provider 呼び出し統計の可視化 |

## 4. ストリーミングイベントのマッピング
- HTTP では 1 行ごとの NDJSON (`event`, `data`, `job_id` 等) を送出している。Adapter はこれを読み取り、MCP の `tool_event` (`{"jsonrpc":"2.0","method":"tool_event","params":{"toolName":"startPipeline","event":"job_status","kind":"status","payload":{...}}}`) として逐次通知する。
- 1 件の `tools/call` リクエスト中に複数の `tool_event` を流し、最後に `tool_result` を返す構成。
- マッピング例:
  - `provider_chunk` → `{"event":"provider_chunk","payload":{"step_id":...,"content":...}}`
  - `item_completed` → `{"event":"artifact","payload":{"result":...}}`
  - `job_status`/`job_completed` etc. → `{"event":"job_status","payload":{...}}`
  - ストリーム終端 (`stream_finished`) で `tool_result` を確定してレスポンスを終了。
- Adapter は NDJSON の `seq`（実装予定）を覚えておき、将来的に resume token を使った `after_seq` にも対応できるようにする。
- `params.kind` は `status` / `chunk` / `result` / `error` を取り、UI 側でカテゴリ別に描画するために利用できる（`provider_chunk`→`chunk`, `item_completed`→`result`, `job_failed` / `step_failed` / `error`→`error`, その他は `status`）。

## 5. Manifest (`pipeforge.mcp.json`) のフォーマット
```jsonc
{
  "name": "pipeforge",
  "description": "Pipeline Engine MCP adapter",
  "commands": [
    {
      "command": "/usr/local/bin/pipeline-engine-mcp",
      "env": {
        "PIPELINE_ENGINE_ADDR": "http://127.0.0.1:8085",
        "PIPELINE_ENGINE_API_TOKEN": "${PIPELINE_ENGINE_API_TOKEN}",
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

## 6. SDK / CLI 実装方針
- **Go Adapter (`cmd/mcp-adapter`)**
  - Go 標準の `encoding/json` で JSON-RPC (initialize / tools/list / tools/call) を解釈し、STDIN/STDOUT でやり取り。
  - `pkg/sdk/go` をラップした `EngineClient` を利用し、Tool ごとに HTTP API へフォワードする。
  - `stream=true` でジョブ開始時は `StreamJobs` のチャンネルを読みながら `tool_event` を逐次送出し、最終的な `tool_result` ではジョブ情報のみを返す。
- **TypeScript Adapter (`pkg/sdk/typescript/src/mcp`)**
  - `@pipeforge/sdk` に `pipeline-engine-mcp` CLI を同梱。Node 18+ で stdio 経由の JSON-RPC を処理し、`PipelineEngineClient` を介して HTTP と連携。
  - `createMCPAdapterClient()` で SDK クライアントを MCPAdapter に接続し、Go 版と同一の Tool 群を提供。
  - `streamJob` / `startPipeline(stream=true)` では `tool_event` 通知をリアルタイムで発火し、最後に `tool_result` を返す構造を踏襲。

## 7. 開発ロードマップ
1. README / ToDo を更新し、MCP 連携の目的とタスクを共有（完了）。
2. `docs/mcp/README.md`（本ドキュメント）を基に、`docs/mcp/AdapterSpec.md` や `docs/mcp/Streaming.md` を段階的に追加。
3. ✅ Go Adapter の PoC を `cmd/mcp-adapter` として追加。`startPipeline` / `streamJob` / `getJob` / `cancelJob` / `rerunJob` / `upsertProviderProfile` を実装済み。
4. TypeScript Adapter を `pkg/sdk/typescript/cmd/mcp` に追加し、npm からインストールできるようにする。
5. CI に MCP Adapter の lint / test を追加。将来的には E2E テストで `pipeline-engine` + Adapter + Mock MCP Host を実行。

## 8. オープンな課題
- 認証: MCP Host から API キーをどう受け渡すか。Manifest の `env` だけでなく、Adapter が `.pipeforge/config.json` などを読む仕組みを検討。
- ストリーム再開: `after_seq` を実装して長時間ジョブでも再接続できるようにする。
- UI 連携: MCP Host によってはカスタムイベント表示が制限されるため、`provider_chunk` と `item_completed` をどう可視化するかのガイドを追加する必要がある。

## 9. 参考
- MCP 仕様: <https://github.com/modelcontextprotocol>
- 既存の README セクション: [README.md#MCP Integration](../../README.md#l547)
- manifest 設定手順: [docs/mcp/ManifestGuide.md](./ManifestGuide.md)
- ストリーム再開仕様: [docs/mcp/StreamingResume.md](./StreamingResume.md)
