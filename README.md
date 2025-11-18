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

**環境変数の使い方**

- OpenAI の場合、`ProviderProfile.APIKey` に直接埋め込むか、環境変数 `PIPELINE_ENGINE_OPENAI_API_KEY` にセットしておくと自動で参照します。`PIPELINE_ENGINE_OPENAI_BASE_URL` / `PIPELINE_ENGINE_OPENAI_MODEL` を指定するとエンドポイントやモデルも切り替えられます。
- `BaseURI` は既定で `https://api.openai.com/v1` ですが、ローカルプロキシやモックサーバーに向けたい場合は上書きできます。
- ローカルの Ollama を利用する場合は `PIPELINE_ENGINE_ENABLE_OLLAMA=1` もしくは `PIPELINE_ENGINE_OLLAMA_BASE_URL` を設定します（既定は `http://127.0.0.1:11434`）。モデルは `PIPELINE_ENGINE_OLLAMA_MODEL` で変更できます。
- ログの出力レベルは `PIPELINE_ENGINE_LOG_LEVEL`（`debug`/`info`/`warn`/`error`）で切り替えられます。未指定時は `info`。

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

## 複数パイプライン例
`engine.BasicEngine` は任意の数の `PipelineDef` を登録できるため、用途別のパイプラインを複数公開できます。例えば OpenAI ベースの要約と、Ollama ベースの分類を同時に扱う場合は以下のように記述します。

```jsonc
[
  {
    "type": "summarize.v0",
    "version": "v1",
    "steps": [
      {
        "id": "summary",
        "kind": "llm",
        "mode": "single",
        "provider_profile_id": "openai-cli",
        "prompt": {
          "user": "Summarize:\n{{range .Sources}}{{.Content}}\n{{end}}"
        },
        "output_type": "markdown",
        "export": true
      }
    ]
  },
  {
    "type": "classify.v0",
    "version": "v1",
    "steps": [
      {
        "id": "classify",
        "kind": "llm",
        "mode": "single",
        "provider_profile_id": "ollama-cli",
        "prompt": {
          "system": "You label feedback strictly in lowercase.",
          "user": "Label this memo: {{index .Sources 0 .Content}}"
        },
        "output_type": "json",
        "export": true
      }
    ]
  }
]
```

`PipelineRegistry.RegisterPipeline` へ順次投入するだけで `/v1/jobs` の `pipeline_type` に両方の値を指定できるようになり、運用中に互いのステップを干渉させることなく拡張できます。

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
| `stream_finished`   | ストリームの終端を通知。以降イベントは届かない |
| `provider_chunk`    | Provider から届く LLM chunk。`StepChunk` として `data` に格納 |
| `error`             | ストリーミング取得中にサーバー側でエラーが発生した場合 |

端末側では `provider_chunk` を受け取っている間に即座に UI へ反映し、`stream_finished` を受信したタイミングで NDJSON の読み取りを終了すれば確実です（その前に `job_completed` / `job_failed` / `job_cancelled` が届きます）。

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

### CLI から OpenAI プロファイルを使ったジョブ実行
OpenAI API キーを設定して `make run` を起動すると、`openai.summarize.v1` パイプラインが自動登録され、`provider_profile_id=openai-cli` が利用できるようになります。

**ターミナル1:**

```bash
export PIPELINE_ENGINE_OPENAI_API_KEY="sk-..."
make run
```

**ターミナル2:**

```bash
curl -s \
  -H "Content-Type: application/json" \
  -d '{
        "pipeline_type": "openai.summarize.v1",
        "input": {
          "sources": [
            { "kind": "note", "label": "memo", "content": "OpenAI 呼び出しテスト" }
          ]
        }
      }' \
  "http://127.0.0.1:8085/v1/jobs?stream=true"
```

`stream=true` を付けておくと NDJSON イベント（`job_queued`, `job_status`, `item_completed` など）を受け取りながら OpenAI の応答をリアルタイムで確認できます。

### CLI から Ollama プロファイルを使ったジョブ実行
ローカルで Ollama サーバー（`ollama serve`）を起動した状態で `PIPELINE_ENGINE_ENABLE_OLLAMA=1` を指定してサーバーを立ち上げると、`ollama.summarize.v1` パイプラインが登録されます。OpenAI と同様に 2 つのターミナルで動作を確認できます。

**ターミナル1:**

```bash
export PIPELINE_ENGINE_ENABLE_OLLAMA=1
# 必要に応じて BASE_URL / MODEL を上書き
# export PIPELINE_ENGINE_OLLAMA_BASE_URL="http://127.0.0.1:11434"
# export PIPELINE_ENGINE_OLLAMA_MODEL="llama3"
make run
```

**ターミナル2:**

```bash
curl -s \
  -H "Content-Type: application/json" \
  -d '{
        "pipeline_type": "ollama.summarize.v1",
        "input": {
          "sources": [
            { "kind": "note", "label": "memo", "content": "Ollama 呼び出しテスト" }
          ]
        }
      }' \
  "http://127.0.0.1:8085/v1/jobs?stream=true"
```

`job_completed`（または `job_failed` / `job_cancelled`）に続いて `stream_finished` が届くまでがワンセットです。`item_completed` には Ollama から返却されたテキストがそのまま入るため、ストリームを読みながら応答を確認できます。

### CLI で複数パイプラインを切り替えて実行
OpenAI / Ollama の両方を有効化しておくと、`pipeline_type` を変えるだけで異なるパイプラインを同じサーバー経由で呼び分けられます。

**ターミナル1:**

```bash
export PIPELINE_ENGINE_OPENAI_API_KEY="sk-..."
export PIPELINE_ENGINE_ENABLE_OLLAMA=1
make run
```

**ターミナル2:**

```bash
# OpenAI summary
curl -s -H "Content-Type: application/json" \
  -d '{
        "pipeline_type": "openai.summarize.v1",
        "input": { "sources": [ { "kind": "note", "content": "まとめたい文章" } ] }
      }' \
  "http://127.0.0.1:8085/v1/jobs?stream=true"

# Ollama classify
curl -s -H "Content-Type: application/json" \
  -d '{
        "pipeline_type": "ollama.summarize.v1",
        "input": { "sources": [ { "kind": "note", "content": "分類対象メモ" } ] }
      }' \
  "http://127.0.0.1:8085/v1/jobs?stream=true"
```

2 つのパイプラインは同一ジョブストアを共有しつつ、各ステップがそれぞれの ProviderProfile を参照するため互いに干渉せずに実行できます。

### ストリーミングで雑学→Markdown 変換を 1 リクエスト実行
`openai.funmarkdown.v1` パイプラインは以下の 3 ステップ構成です。
1. **Random Trivia**: 口語の導入文と `タイトル:/まとめ:/理由:/ディテール:` ラベル付きテキストを生成
2. **Enrich Trivia**: 1 の内容を引き継ぎ、背景説明や追加のトリビアを挿入して厚みを出す
3. **Format Trivia**: 2 のラベルを解析し、「## 見出し」「> 要約」「### ポイント」「### ディテール」を備えた Markdown に整形

すべてのステップが `export=true` になっているため、`stream=true` を付けると口語アウトライン → 深掘り → Markdown という流れを順番に受け取れます。

```bash
export PIPELINE_ENGINE_OPENAI_API_KEY="sk-..."
make run

curl -N -H "Content-Type: application/json" \
  -d '{
        "pipeline_type": "openai.funmarkdown.v1",
        "input": {
          "sources": [ { "kind": "note", "content": "雑学を話して" } ]
        }
      }' \
  "http://127.0.0.1:8085/v1/jobs?stream=true"
```

代表的なレスポンス（抜粋、および [StreamingEvents](docs/api/StreamingEvents.md) 参照）:

```jsonc
{"event":"step_started","data":{"step_id":"trivia"}}
{"event":"provider_chunk","data":{"step_id":"trivia","index":0,"content":"そういえば、富士山には…"}}
{"event":"item_completed","data":{"label":"default","step_id":"trivia","data":{"text":"そういえば、富士山には面白い逸話がありまして…\nタイトル: 富士山の余談\nまとめ: ..."}}}
{"event":"step_started","data":{"step_id":"enrich"}}
{"event":"provider_chunk","data":{"step_id":"enrich","index":0,"content":"富士山の余談をさらに掘り下げると…"}}
{"event":"item_completed","data":{"label":"default","step_id":"enrich","data":{"text":"そういえば、富士山には面白い逸話がありまして…(詳細版)\nタイトル: 富士山の余談\nまとめ: ...（追記あり）"}}}
{"event":"step_started","data":{"step_id":"markdown"}}
{"event":"provider_chunk","data":{"step_id":"markdown","index":0,"content":"## 富士山の余談\n> 要約…"}}
{"event":"item_completed","data":{"label":"default","step_id":"markdown","data":{"text":"## 富士山の余談\n> 要約…\n\n### ポイント\n1. …\n\n### ディテール\n- …"}}}
{"event":"job_completed","data":{"status":"succeeded"}}
{"event":"stream_finished","data":{"status":"succeeded"}}
```

このように 1 つのリクエストで生成→整形のパイプラインを構築でき、ストリーミングで各段階の応答をフロントエンドへ即時配信できます。

### Go SDK で chunk を処理する例

```go
client := gosdk.NewClient("http://127.0.0.1:8085")
events, job, err := client.StreamJobs(ctx, engine.JobRequest{PipelineType: "openai.funmarkdown.v1"})
if err != nil {
    log.Fatal(err)
}
for evt := range events {
    switch evt.Event {
    case "provider_chunk":
        if chunk, ok := evt.Data.(map[string]any); ok {
            fmt.Printf("chunk %s: %s\n", chunk["step_id"], chunk["content"])
        }
    case "item_completed":
        fmt.Println("step done", evt.Data)
    case "stream_finished":
        fmt.Println("stream completed for job", evt.JobID)
    }
}
fmt.Println("job accepted:", job.ID)
```

### CLI でパイプラインを連結（OpenAI → OpenAI）
`mode":"sync"` を指定するとジョブ完了まで待って結果を返すため、1 回目の結果をそのまま次のパイプラインに渡すシンプルな bash スクリプトが書けます。下記は要約 → 校正の 2 段を OpenAI パイプラインで直列実行する例です。

```bash
export PIPELINE_ENGINE_OPENAI_API_KEY="sk-..."

# 1. summarize.v1 で入力を要約
SUMMARY=$(curl -s -H "Content-Type: application/json" \
  -d @- http://127.0.0.1:8085/v1/jobs <<'JSON' | jq -r '.result.items[0].data.text'
{
  "mode": "sync",
  "pipeline_type": "openai.summarize.v1",
  "input": {
    "sources": [ { "kind": "note", "content": "この文章を要約してから校正したい" } ]
  }
}
JSON
)

# 2. summarize.v1 を再利用して校正用プロンプトを流用
curl -s -H "Content-Type: application/json" \
  -d @- http://127.0.0.1:8085/v1/jobs <<JSON | jq -r '.result.items[0].data.text'
{
  "mode": "sync",
  "pipeline_type": "openai.summarize.v1",
  "input": {
    "sources": [ { "kind": "note", "content": "校正して:\n$SUMMARY" } ]
  }
}
JSON
```

ここでは同じ `openai.summarize.v1` を 2 度呼び出していますが、別種のパイプラインを組み合わせても問題なく動作します。`SUMMARY` 変数に格納されるテキストは `jq` で抽出しているため、そのまま任意のコマンドに渡せます。

### 単一リクエストで連結（`openai.chain.v1`）
OpenAI プロファイルが有効な場合はデモ用に `openai.chain.v1` パイプラインも自動登録されます。これは Step1 で要約、Step2 で校正する 2 ノード構成になっており、1 回の API リクエストで連結処理を実現します。

```bash
export PIPELINE_ENGINE_OPENAI_API_KEY="sk-..."
make run

curl -s \
  -H "Content-Type: application/json" \
  -d '{
        "mode": "sync",
        "pipeline_type": "openai.chain.v1",
        "input": {
          "sources": [
            { "kind": "note", "label": "memo", "content": "この記事を 3 行でまとめ、最後に丁寧語で校正してください" }
          ]
        }
      }' \
  "http://127.0.0.1:8085/v1/jobs"
```

レスポンスの `result.items[0]` には Step2（校正）の出力のみが格納され、Step1 の要約は内部で依存関係として利用されます。テンプレートでは `{{with index .Previous "summarize"}}...{{end}}` のように前段ステップの結果へアクセスできるため、さらに複雑な連結処理も 1 つのパイプライン型としてまとめられます。

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

## メトリクス / ログ
- `PIPELINE_ENGINE_LOG_LEVEL` で `debug`/`info`/`warn`/`error` のログレベルを指定できます（既定 `info`）。
- Go の `expvar` を利用してメトリクスを `/debug/vars` で公開しています。主なキー:
  - `provider_call_count` / `provider_call_latency_ms` / `provider_call_errors`: Provider 呼び出し回数・総レイテンシ・エラー数（kind 別）
  - `provider_chunk_count`: Provider chunk 送出数
- chunk イベントは `provider_chunk` としてストリーミング中に届くので、UI 側はこれを逐次描画し、`stream_finished` 受信時にストリームを閉じてください。

## ライセンス
本リポジトリは [Apache License 2.0](LICENSE) の下で提供されています。
