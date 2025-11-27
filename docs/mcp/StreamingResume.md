# Streaming Resume Design (`after_seq` / resume token)

長時間ジョブやネットワーク断の際にストリームを再接続し、欠落したイベントのみ再取得できるようにするための設計メモです。

## 1. ゴール
- `/v1/jobs?stream=true` および `/v1/jobs/{id}/stream` で **単調増加するシーケンス番号 (`seq`)** を付与し、途中からの再取得を可能にする。
- MCP アダプタ経由でも `after_seq` を指定できるようにし、`tool_event` 側にも `seq` を転送して UI が欠落判定を行えるようにする。
- 再接続時に **最小限のイベント再送**（`after_seq` より大きいもののみ）を行い、不要な重複表示を抑える。

## 2. HTTP API 拡張案
### 2.1 StreamingEvent 拡張
すべてのイベントに `seq` フィールドを追加（`uint64` 推奨）。`job_queued` を `seq=1` とし、イベントごとに +1。

```jsonc
{"event":"job_status","job_id":"job-1","seq":5,"data":{"status":"running"}}
```

### 2.2 `/v1/jobs?stream=true`
- レスポンス 1 行目の `job_queued` に `seq=1` を含める。
- クライアントが最後に受信した `seq` を `resume_token` として保持できるよう、終端イベント（`stream_finished` / `job_failed` など）で `next_seq` を通知する拡張を検討。

### 2.3 `/v1/jobs/{id}/stream?after_seq=N`
- 新しいクエリパラメータ `after_seq` を受け付ける。省略時は 0（全イベント）。
- サーバーは `after_seq` より大きい `seq` のイベントのみ送信する。
- 新規に `GET /v1/jobs/{id}/events?after_seq=N&limit=M` のようなポーリング API を用意する案も検討（UI が HTTP chunk を扱えない場合に備える）。

### 2.4 resume token 形式
HTTP レイヤーでは `{ "job_id": "...", "seq": 42 }` のような JSON をレスポンスに含め、クライアントはこれを保存しておく。`stream_finished` の `data` に `{"resume_token":{"job_id":"...","seq":42}}` を含めるイメージ。

## 3. 内部実装メモ
- `engine.StreamingEvent` に `Seq uint64` を追加。`BasicEngine.streamJob` でイベント生成時にインクリメント。
- 既存の `MemoryStore` にはイベント履歴が保持されていないため、`Job` ごとのイベントリングバッファをエンジン側で保持する必要あり（例: 最新 1000 件）。ストレージを拡張するまでは「ジョブ完了後の再取得」用途に限定してもよい。
- `after_seq` が履歴範囲を超える場合は `HTTP 410 Gone` を返し、クライアントにフルリプレイを促す。

## 4. MCP アダプタへの反映
### 4.1 `tool_event` への `seq`
- Go / TypeScript 両アダプタで `StreamingEvent.seq` を `tool_event.params.seq` に含める。
- イベント整合性を保つため `tool_event.params.kind` と組み合わせて UI が欠落検知可能にする。

### 4.2 `streamJob` ツールの引数
- 既存引数 `{ "job_id": "..." }` に `after_seq?: number` を追加。Go アダプタは `/v1/jobs/{id}/stream?after_seq=...` を呼び出す。
- 再接続専用の `resume_token` を渡せるよう、`{"job_id":"...","resume_token":{"seq":42}}` など複合型にする案もあるが、初期は `after_seq` のみにしてクライアント側で job_id を保持してもらう。

### 4.3 `startPipeline(stream=true)` の `tool_result`
- ストリーム完了時の `tool_result.job` に加えて `resume_token`（`{ "job_id": job.ID, "seq": lastSeq }`）を返し、UI が次回 `streamJob` で `after_seq` を設定できるようにする。

## 5. UI / クライアント対応
- `PipelineEngineClient` (Go/TS) に `lastSeq` を返す API を追加し、アプリが途中から再生する際に利用できるようにする。
- MCP ホストが `tool_event` を順番に描画する際、欠落検知（`currentSeq + 1 != incomingSeq`）で警告を出す UI も実装しやすくなる。

## 6. ロードマップ反映
1. `engine.StreamingEvent` / HTTP レスポンスに `seq` 追加。
2. `after_seq` クエリパラメータを `/v1/jobs/{id}/stream` に実装。
3. イベント履歴バッファ（in-memory → 永続化へ拡張）。
4. MCP アダプタ＆SDK に `seq` / `resume_token` を配信。
5. ドキュメント更新（README / docs/mcp/StreamingResume.md）。

これらの仕様をベースに実装を段階的に進め、最終的には「ジョブ実行中にネットワーク断があっても `after_seq` で再接続し、欠落なく UI を更新できる」状態を目指す。
