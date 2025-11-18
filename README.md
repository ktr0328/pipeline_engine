# Pipeline Engine

ローカルマシン上で常駐／組み込みのどちらでも扱える AI パイプライン実行エンジンです。ログ・コード・ノートなど複数種類の入力を受け取り、OpenAI や Ollama などの Provider ノードを直列 / 並列に組み合わせたパイプラインを実行し、結果をストリーミングしながら返します。初期バージョンではシングルノード実装に絞り、DAG オーケストレーションや Provider 連携を段階的に拡張していきます。

## テスト
- `internal/engine/engine_test.go`: BasicEngine と MemoryStore の疎通、キャンセル、ストリーミングイベントを検証します。
- `internal/store/memory_test.go`: MemoryStore の Create / Update / Get / List とディープコピー動作を確認します。
- `internal/server/handlers_test.go`: HTTP ハンドラがヘルスチェックや `/v1/jobs`, `/v1/jobs/{id}/cancel` などで正しいレスポンスを返すかを確認します。

テストは以下のコマンドで実行できます。

```bash
# ルートディレクトリで実行
go test ./...
```

## 主な特徴
- Go 製の軽量エンジン (`internal/engine`) と HTTP サーバー (`internal/server`) を同一バイナリで提供。
- Provider 抽象（OpenAI / Ollama / 画像生成 / ローカルツール等）と構造化プロンプト `PromptTemplate` を備えた Step 定義。
- `Job` / `StepExecution` / `ResultItem` などのドメインモデルで進捗と最終結果を管理し、NDJSON ストリーミングで途中経過を配信。
- `/v1/jobs/{id}/cancel` で実行中ジョブのキャンセル、`/v1/jobs/{id}/rerun` で途中ステップからのリランに対応予定（v0 ではスタブ実装）。
- デフォルトではインメモリストア (`internal/store/memory.go`) を利用し、外部永続化層の差し替えも容易。

## リポジトリ構成
```
cmd/pipeline-engine   # 実行エントリーポイント。
docs/                # 詳細設計書・計画メモ。
internal/engine      # Job / Pipeline 実行ロジック。
internal/server      # HTTP ハンドラと NDJSON ストリーミング。
internal/store       # Job を保持するストア実装 (MemoryStore)。
pkg/                 # 共有ライブラリを追加予定の空ディレクトリ。
```

## プロバイダ設定例
`engine.NewBasicEngineWithConfig` に `EngineConfig` を渡すことで、OpenAI や Ollama など複数の ProviderProfile を登録できます。Step 定義側で `provider_profile_id` と `provider_override` を指定すると、プロファイルの値を上書きして特定のモデルやエンドポイントを利用できます。

**API キーの渡し方**

- OpenAI の場合、`ProviderProfile.APIKey` に直接埋め込むか、環境変数 `PIPELINE_ENGINE_OPENAI_API_KEY` にセットしておくと自動で参照します。
- `BaseURI` は既定で `https://api.openai.com/v1` ですが、ローカルプロキシやモックサーバーに向けたい場合は上書きできます。
- Ollama など他プロバイダも同様に、将来的に環境変数でキーを渡せるように設計予定です。

```go
cfg := &engine.EngineConfig{
    Providers: []engine.ProviderProfile{
        {
            ID:           engine.ProviderProfileID("openai-main"),
            Kind:         engine.ProviderOpenAI,
            BaseURI:      "https://api.openai.com/v1",
            APIKey:       os.Getenv("OPENAI_API_KEY"),
            DefaultModel: "gpt-4o-mini",
        },
        {
            ID:           engine.ProviderProfileID("ollama-local"),
            Kind:         engine.ProviderOllama,
            BaseURI:      "http://127.0.0.1:11434",
            DefaultModel: "llama3",
        },
    },
}
eng := engine.NewBasicEngineWithConfig(store.NewMemoryStore(), cfg)
```

Step 側の例:

```json
{
  "id": "final_summary",
  "kind": "llm",
  "mode": "single",
  "provider_profile_id": "openai-main",
  "provider_override": {
    "default_model": "gpt-4o"
  },
  "prompt": {
    "system": "You are a helpful assistant.",
    "user": "Summarize the context: {{range .Sources}}{{.Content}}\n{{end}}"
  },
  "output_type": "markdown",
  "export": true
}
```

## DAG / パイプライン例
複数 Step を `depends_on` で繋ぐことで DAG を表現します。下記はログ解析 → サマリー生成 → レポート整形の 3 ステップ構成です。

```json
{
  "type": "system_log_analysis",
  "version": "v1",
  "steps": [
    {
      "id": "parse_logs",
      "name": "Parse Logs",
      "kind": "map",
      "mode": "fanout",
      "output_type": "text",
      "export": true
    },
    {
      "id": "summarize",
      "name": "Summarize",
      "kind": "llm",
      "mode": "per_item",
      "depends_on": ["parse_logs"],
      "provider_profile_id": "openai-main",
      "output_type": "markdown",
      "export": true
    },
    {
      "id": "report",
      "name": "Report",
      "kind": "llm",
      "depends_on": ["summarize"],
      "provider_profile_id": "openai-main",
      "output_type": "markdown",
      "export": true
    }
  ]
}
```

## セットアップ
1. Go 1.22 以降を用意します。
2. 依存関係は `go.mod` の標準ライブラリのみなので追加の `go mod download` は不要です。
3. サーバーを起動します（`make run` でも可）。

```bash
go run ./cmd/pipeline-engine
# PIPELINE_ENGINE_ADDR="127.0.0.1:9000" go run ./cmd/pipeline-engine で待受ポートを変更できます。
```

### よく使う Make タスク

| コマンド | 説明 |
| ------ | ---- |
| `make test` | `go test ./...` を実行 |
| `make run` | `PIPELINE_ENGINE_ADDR` を指定してサーバーを起動 |
| `make dev` | テストを実行後、そのままサーバーを起動（簡易開発ループ） |
| `make build` | CLI バイナリをビルド |

## クイックスタート
### ヘルスチェック
```bash
curl -s http://127.0.0.1:8085/health
# => {"status":"ok","version":"0.2.0","uptime_sec":1.23}
```

### ジョブの作成
`POST /v1/jobs` にパイプライン種別と入力を渡すと、非同期ジョブがキューに登録されます。下記はログとメモを入力にした例です。

```bash
curl -s \
  -H "Content-Type: application/json" \
  -d @- http://127.0.0.1:8085/v1/jobs <<'JSON'
{
  "pipeline_type": "summarize.v0",
  "input": {
    "sources": [
      { "kind": "log", "label": "web", "content": "[INFO] handler started" },
      { "kind": "note", "label": "memo", "content": "調査対象: pipeline engine" }
    ],
    "options": {
      "language": "ja",
      "detail_level": "short"
    }
  }
}
JSON
```

レスポンスは `job` オブジェクトを含む JSON で、ID を `GET /v1/jobs/{id}` に渡すことで最終結果を再取得できます。

```bash
curl -s http://127.0.0.1:8085/v1/jobs/0123456789abcdef
```

### ストリーミング実行
ストリームで途中経過を取得する場合は `stream=true` を付与します。レスポンスは 1 行 1 イベントの NDJSON です。

```bash
curl -N -H "Content-Type: application/json" \
  "http://127.0.0.1:8085/v1/jobs?stream=true" \
  -d '{"pipeline_type":"summarize.v0","input":{"sources":[]}}'
```

`/v1/jobs/{id}/stream` に対して GET することで、既存ジョブのステータスを監視することもできます。代表的なイベント種別は以下の通りです。

| Event 名            | 説明 |
| ------------------- | ---- |
| `job_status`        | ジョブの状態が変化したときに送出されるフル状態 |
| `job_started`       | `queued -> running` の遷移時に 1 度だけ送出 |
| `job_completed`     | 正常終了。失敗・キャンセル時は `job_failed` / `job_cancelled` |
| `step_started`      | 各 StepExecution が `running` になったタイミング |
| `step_completed`    | StepExecution が `success` で完了したタイミング（失敗時は `step_failed`） |
| `item_completed`    | Export 指定された ResultItem が生成されるたびに送出 |
| `error`             | ストリーミング取得中にサーバー側でエラーが発生した場合 |

### キャンセルとリラン
```bash
# キャンセル
curl -X POST -H "Content-Type: application/json" \
  -d '{"reason":"user aborted"}' \
  http://127.0.0.1:8085/v1/jobs/{id}/cancel

# 特定ステップからの再実行（from_step_id や reuse_upstream を指定可能）
curl -X POST -H "Content-Type: application/json" \
  -d '{"from_step_id":"step-2","reuse_upstream":true}' \
  http://127.0.0.1:8085/v1/jobs/{id}/rerun
```

## API サマリー
| Method | Path | 説明 |
| ------ | ---- | ---- |
| `GET` | `/health` | エンジンの稼働確認 |
| `POST` | `/v1/jobs` | ジョブの作成。`stream=true` で NDJSON ストリーム |
| `GET` | `/v1/jobs/{id}` | ジョブ詳細と結果の取得 |
| `GET` | `/v1/jobs/{id}/stream` | 既存ジョブのステータス変化をストリームで受信 |
| `POST` | `/v1/jobs/{id}/cancel` | 実行中ジョブのキャンセル |
| `POST` | `/v1/jobs/{id}/rerun` | 同じ入力を使ったリラン、または途中ステップからの再実行 |

## ドメインモデルの抜粋
- **Provider / ProviderProfile**: OpenAI や Ollama、画像生成などの外部実行体を `ProviderKind` として抽象化。Step ごとに `ProviderOverride` を与えることでモデルやエンドポイントを上書きできます。
- **StepDef**: `kind`（LLM/Image/Map/Reduce/Custom）、`mode`（single/fanout/per_item）、`prompt`、`output_type` などを保持するパイプラインノード。DAG 依存関係は `depends_on` で表現します。
- **Job / StepExecution / ResultItem**: `JobStatus`（queued/running/succeeded/failed/cancelled）を持ち、各ステップの開始・終了時刻や結果を追跡します。`ResultItem` は `content_type` (text, markdown, json...) と任意の `data` を保持します。
- **StreamingEvent**: `event` 名と `job` 情報、エラー文字列などを 1 行ずつクライアントへ送信するための構造体です。

詳細は `docs/詳細設計書.md` にまとめています。

## 今後のロードマップ
- DAG スケジューラ、チェックポイント、複数ノードの並列実行。
- Provider SDK（TypeScript / Python / Go）とアプリケーション層（常駐サジェスタ、フローベース UI など）。
- Unix ドメインソケット対応や永続ストアのプラガブル化。
- 途中ステップからの厳密なリラン、ストリーム再開などの運用機能強化。

## ライセンス
本リポジトリは [Apache License 2.0](LICENSE) の下で提供されています。
