# MCP Manifest & Setup Guide

Claude Desktop や Cursor などの MCP ホストから `pipeline-engine` を利用する際の manifest 設定手順をまとめます。

## 1. 前提
- Pipeline Engine をローカルで起動済み（例: `PIPELINE_ENGINE_ADDR=http://127.0.0.1:8085`）。
- MCP アダプタ CLI (`pipeline-engine-mcp`) をビルド済み、もしくは `@pipeforge/sdk` の npm CLI をインストール済み。
  - Go 版: `go build -o ~/bin/pipeline-engine-mcp ./cmd/mcp-adapter`
  - TypeScript 版: `npm install -g @pipeforge/sdk` で `pipeline-engine-mcp` コマンドを利用可能。

## 2. Manifest テンプレ (`pipeforge.mcp.json`)

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

### 2.1 環境変数の扱い
- `PIPELINE_ENGINE_ADDR`: 連携先の HTTP API ベース URL。デフォルト `http://127.0.0.1:8085`。
- `PIPELINE_ENGINE_API_TOKEN`: 認証が必要な場合の API トークン（未使用なら空で可）。
- `PIPELINE_ENGINE_OPENAI_API_KEY` など Provider 用のキーを `env` に渡しておくと、アダプタ起動時に `os.Getenv` や Node の `process.env` から利用できます。
- manifest 内で `${VAR_NAME}` を指定すると MCP ホストがユーザー入力を促すケースが多いため、秘密情報は manifest 側で参照する形がおすすめです。

### 2.2 `commands` のパス
- Go 版: ビルドしたバイナリを `/usr/local/bin/pipeline-engine-mcp` など PATH に置いておくと他ホストから再利用しやすいです。
- TypeScript 版: `npm install -g @pipeforge/sdk` で `pipeline-engine-mcp` が `PATH` に入るため、そのまま `\"command\": \"pipeline-engine-mcp\"` と記述可能。

## 3. MCP ホストへの登録例
### 3.1 Claude Desktop
1. `~/Library/Application Support/Claude/pipeforge.mcp.json` を作成し上記テンプレを書き込む。
2. Claude Desktop を再起動し、設定 > MCP で `pipeforge` を有効化。
3. `PIPELINE_ENGINE_ADDR` 等が `${...}` の場合は UI から値入力を求められる。

### 3.2 Cursor / VS Code MCP プラグイン
- `.cursor/mcp/pipeforge.mcp.json` や `.mcp/` 配下に同じ manifest を置き、エディタの再起動後に接続確認を行う。
- ログ等は `~/.cursor/mcp.log` や VS Code の出力タブで確認可能。

## 4. 動作確認
1. MCP ホストから `startPipeline` ツールを呼び出し、`pipeline_type` と `input` を指定してジョブが作成できるか確認。
2. `streamJob`/`startPipeline(stream=true)` 実行時に `tool_event` がリアルタイムで流れるかをホスト UI 上でチェック。`params.kind` 値（`status`/`chunk`/`result`/`error`）を使ってトークン描画や結果表示を実装できる。
3. API キーの入れ替えは `upsertProviderProfile` を使い、OpenAI / Ollama などの設定が反映されるか `GET /v1/config/providers` で確認。

## 5. トラブルシュート
- **`command not found`**: manifest の `command` パスが正しいか、実行権限（`chmod +x`）があるか確認。
- **`http error 404`**: `PIPELINE_ENGINE_ADDR` が誤っている可能性。`curl $PIPELINE_ENGINE_ADDR/health` を実行して疎通確認。
- **イベントが届かない**: `pipeline-engine` 側のログで NDJSON の出力有無を確認。`tool_event` が届いているか MCP ホストの開発者コンソールで確認。

## 6. 今後の拡張予定
- manifest テンプレに `listPipelines` など追加ツールのサンプルを順次追記。
- `pipeforge.mcp.json` を `docs/mcp/examples/` 配下に OS 別テンプレとして提供予定。
