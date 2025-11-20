# Pipeline Engine v0.2 – 実装計画

## ターゲット
- Go 1.22+
- Linux / ローカル環境
- モノリポ1個（将来的に分割しても良い構成）

## 進捗サマリー（2024-XX 更新）
- ✅ ドメインモデル / MemoryStore / HTTP エンドポイントは v0.2 仕様で実装済み。
- ✅ DAG 実行・PromptTemplate 適用・ProviderRegistry（OpenAI / Ollama / Image / LocalTool のスタブ）をエンジンに実装済み。
- ✅ StreamingEvent の粒度拡張（`job_started`/`step_completed`/`item_completed` など）と `/health` の version / uptime 返却を実装。
- ✅ Go SDK (`pkg/sdk/go`) で Create/Get/Cancel/Rerun/Stream API をサポート。単体テストを追加済み。
- ✅ MemoryStore での StepCheckpoint 永続化、および TypeScript/Python SDK 設計メモの追加を完了。
- 🔄 次フェーズ: Provider 実 API 呼び出し実装（OpenAI / Ollama との連携強化）と追加 SDK 実装。

## 0. ディレクトリ構成（最初のゴール）
- `cmd/pipeline-engine/main.go`
- `internal/engine/engine.go`
- `internal/engine/types.go`
- `internal/engine/scheduler.go`
- `internal/server/http.go`
- `internal/server/handlers.go`
- `internal/store/memory.go`  (Job / StepExecution / StepCheckpoint のインメモリ実装)
- `pkg/sdk/go/client.go`      (Goクライアントの薄いラッパ)

## 1. ドメインモデルの実装
`internal/engine/types.go` に以下を定義:
- ProviderKind / ProviderProfile
- ContentType / OutputFormat
- PromptTemplate
- StepKind / StepMode / StepDef / PipelineDef
- SourceKind / Source
- JobOptions / JobInput
- ResultItem / JobResult
- JobStatus / Job / JobError
- StepExecutionStatus / StepExecution
- StepCheckpoint

これらは spec のフィールドをそのまま struct にする。
JSONタグは `json:"snake_case"` で揃える。

### 注意
- `map[string]any` は `map[string]any` でOK。
- 時刻は `time.Time` を使う（JSONはRFC3339）。

## 2. Engine インターフェース
`internal/engine/engine.go` にインターフェースと基本実装:

```go
type JobRequest struct {
    PipelineType PipelineType `json:"pipeline_type"`
    Input        JobInput     `json:"input"`
    Mode         string       `json:"mode,omitempty"` // "sync" | "async"
}

type Engine interface {
    RunJob(ctx context.Context, req JobRequest) (*Job, error)
    RunJobStream(ctx context.Context, req JobRequest) (<-chan StreamingEvent, *Job, error)
    CancelJob(ctx context.Context, jobID string, reason string) error
    GetJob(ctx context.Context, jobID string) (*Job, error)
}

type StreamingEvent struct {
    Event string      `json:"event"`
    JobID string      `json:"job_id"`
    Data  interface{} `json:"data"`
}
```

最初の v0 では:
- パイプライン実行は「ダミー実装」でもよい（1ステップだけ実行、など）
- とりあえず Job を作って queued -> running -> succeeded に遷移させる
- ResultItem も適当な固定値でOK

## 3. Job ストア（メモリ）
`internal/store/memory.go`:

```go
type MemoryStore struct {
    jobs map[string]*Job
    mu   sync.RWMutex
}
```

メソッド:
- CreateJob(job *Job) error
- UpdateJob(job *Job) error
- GetJob(id string) (*Job, error)
- ListJobs() ([]*Job, error) (必要になれば)

StepExecution / StepCheckpoint はとりあえず Job に内包でOK（別テーブルに分けなくていい）

## 4. HTTP サーバー
`internal/server/http.go`:

```go
type Server struct {
    engine engine.Engine
}

func NewServer(e engine.Engine) *Server
func (s *Server) ListenAndServe(addr string) error
```

ルーターは標準ライブラリ net/http + http.ServeMux でOK（chi や gin は不要）。

`internal/server/handlers.go` にエンドポイント:
- GET /health
- POST /v1/jobs
- GET /v1/jobs/{id}
- GET /v1/jobs/{id}/stream
- POST /v1/jobs/{id}/rerun （中身は後ででOK。とりあえず 501 Not Implemented でもよい。）
- POST /v1/jobs/{id}/cancel

レスポンスは spec にある JSON フォーマットにできるだけ近づける。

## 5. main.go
`cmd/pipeline-engine/main.go`:

```go
func main() {
    MemoryStore を初期化
    engine.NewEngine(store, config) みたいなのを作成
    server.NewServer(engine) を起動
    ListenAndServe(":8085")
}
```

## 6. PromptTemplate と Provider 実装の雛形
最初の段階では実際の OpenAI / Ollama 呼び出しは不要。
代わりに「ここに叩くコードを書く」という TODO 付きのインターフェースだけを作る。

例: `internal/engine/provider.go`:

```go
type Provider interface {
    CallLLM(ctx context.Context, step StepDef, input string) (string, error)
    // 画像系など将来用のメソッドは TODO
}

type ProviderRegistry struct {
    providers map[ProviderProfileID]Provider
}
```

Ollama 用のダミー実装:

```go
type OllamaProvider struct {
    BaseURI string
    Model   string
}

func (p *OllamaProvider) CallLLM(ctx context.Context, step StepDef, input string) (string, error) {
    // TODO: PromptTemplate を使って input を組み立てる
    // TODO: Ollama の HTTP API を叩く
    // v0は単純に "dummy response" を返してもよい
    return "dummy response from ollama", nil
}
```

## 7. キャンセル・ストリーミング
Engine 内部実装では Job ごとに context.Context を持たせる:
- RunJob / RunJobStream を呼ばれたときに jobCtx, cancel := context.WithCancel(rootCtx)
- CancelJob では Job に紐づく cancel() を呼ぶ
- 実際の Step 実行／LLM呼び出しは jobCtx を使う

ストリーミングは:
- RunJobStream が chan StreamingEvent を返す
- HTTP ハンドラはそれを読みながら application/x-ndjson で書き出す
- 切断されても Job は続行する。ジョブの最終状態は GET /v1/jobs/{id} で取得する。

## 8. 最初のマイルストーン
- types.go に全 struct を定義
- MemoryStore 実装
- Engine の「ダミー実行」実装（1ステップ / 固定レスポンス）
- HTTP サーバー + 全エンドポイント実装（中身一部ダミー）

curl で以下を確認:
- POST /v1/jobs → job が作成される
- GET /v1/jobs/{id} → job が取得できる
- POST /v1/jobs/{id}/cancel → status が cancelled になる
- GET /v1/jobs/{id}/stream → dummy のイベントが流れる

ここまで動けば、次のステップで:
- DAG スケジューラ
- 実際の Ollama 呼び出し
- PromptTemplate の適用
を追加していく。
