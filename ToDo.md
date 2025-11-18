# ToDo

本リストは README.md / docs/詳細設計書.md / docs/plans/ImplementationPlan.md を元にしたロードマップです。

## ✅ ベースライン (完了)
- [x] ドメインモデル定義 (`internal/engine/types.go`)。Provider / Step / Job / Result / StreamingEvent など基本型を実装済み。
- [x] インメモリ Job ストア (`internal/store/memory.go`) と BasicEngine のジョブ生成〜ダミー実行〜ストリーミング骨格。
- [x] HTTP サーバー (`internal/server`) と `/health`, `/v1/jobs`, `/v1/jobs/{id}` ほか基本エンドポイントの実装。

## 🔜 v0.2 マイルストーン
### Engine / Scheduler
- [x] BasicEngine の単一ステップ実装を、`StepDef.DependsOn` に従う DAG 実行へ刷新 (docs/詳細設計書.md §6.2)。
- [x] `StepMode` 別（single / fanout / per_item）のワークフローとシリアル実装（将来の並列化のベース）を導入。
- [x] `StepExecution` / `StepCheckpoint` を使い、途中結果の保存と `rerun` API の `from_step_id` / `reuse_upstream` を尊重（チェックポイントはエンジン内メモリ保持。永続化は別タスク）。
- [x] `PromptTemplate` を入力にバインドして Provider へ渡す共通ロジックを導入 (docs/詳細設計書.md §3.2, §6.3)。
- [x] `JobRequest.Mode` の sync 実行パスとレスポンス分岐を整備 (docs/詳細設計書.md §5.2)。

### Provider 層
- [x] ProviderRegistry に OpenAI / Ollama / Image / LocalTool プロバイダを登録・切替できる設定レイヤを実装。
- [ ] OllamaProvider / OpenAIProvider で実 API 呼び出しを実装し、リトライ・タイムアウト・エラーハンドリングを付与。
- [x] Step ごとの `ProviderOverride` を ProviderProfile にマージする解決順序（ImplementationPlan §1）を実装。
- [x] 画像/ローカルツール StepKind 用のインターフェースとダミー（もしくは CLI 呼び出し）実装を追加。

### ストア / 永続化
- [x] MemoryStore を拡張し、`StepCheckpoint` を保持・再利用する API を追加。
- [ ] オプションでファイル or SQLite バックエンドに差し替えられるストアインターフェースを設計。
- [ ] Job / StepExecution の `clone` ロジックを統合テストし、データ破損や共有参照を防ぐ。

### HTTP / ストリーミング
- [x] `/health` 応答に version / uptime を追加 (docs/詳細設計書.md §5.1)。
- [x] `/v1/jobs` の `stream=true` と `/v1/jobs/{id}/stream` で StreamingEvent (`job_started`, `item_completed` など) を網羅的に送出。
- [x] `/v1/jobs/{id}/rerun` の `override_input` を反映し、`parent_job_id` を設定した新規 Job を返す。
- [x] API エラー形式 `{ error: { code, message, details } }` を全エンドポイントで統一。

### SDK / クライアント
- [x] `pkg/sdk/go/client.go` に HTTP クライアント（CreateJob / GetJob / Stream / Cancel / Rerun）を実装。
- [x] curl 以外の利用者向けに TypeScript / Python SDK 設計メモを追加。

### テスト / DevEx
- [x] Engine / store / HTTP ハンドラ向けのユニットテストを整備し、`go test ./...` で基本的な動作確認が可能。
- [x] `make dev` などのタスクランナー（lint, test, run）を準備。

## 🗺️ ミドルターム (README ロードマップ参照)
- [ ] DAG スケジューラの高度化：複数ノード並列、リトライ、ステップ毎のタイムアウト設定。
- [ ] Provider SDK (TypeScript / Python / Go) の公開とリポジトリ分割検討。
- [ ] Unix ドメインソケットのリスニングサポート＆設定 (`PIPELINE_ENGINE_ADDR` で UNIX ソケット指定)。
- [ ] 永続ストアのプラガブル化（PostgreSQL / SQLite / BoltDB 等）。
- [ ] 厳密なリラン（途中ステップからの再実行と upstream 再利用）、ストリーム再開 (after_seq) の実装。
- [ ] Prompt / Pipeline 定義ファイルのホットリロードと設定管理。

## 🌅 ロングターム / 研究課題 (docs/詳細設計書.md §8)
- [ ] ストリーム再開 API の仕様固め＋実装 (resume token / after_seq)。
- [ ] StepCheckpoint 永続化の差し替え（S3 / SQLite など）とガーベジコレクション戦略。
- [ ] ローカル API キー等の認証・アクセス制御。
- [ ] フローエディタやノート連携ツールとの NodeBinding 機能（PromptTemplate / ProviderProfile の GUI 編集）。
- [ ] 常駐サジェスタアプリ / フローベース UI / CLI 連携ツールの試作。

## 📚 ドキュメントタスク
- [x] README に Provider 設定例と DAG 例、ストリームイベント仕様を追記。
- [x] `docs/詳細設計書.md` を最新実装へ随時更新（特に §5 HTTP API と §6 Engine 挙動）。
- [x] ImplementationPlan をアップデートし、完了タスクと次フェーズを明示。
