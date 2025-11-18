# Pipeline Engine System Diagram

本ファイルでは、Pipeline Engine の主要コンポーネントと内部データフローを簡潔に図解します。

```mermaid
graph TD
    subgraph Clients
        A[CLI / 常駐アプリ]
        B[Web / Electron UI]
        C[SDK - Go/TS/Python]
    end

    subgraph HTTP Interface
        D[HTTP Server\ninternal/server]
        D -->|REST / NDJSON| E[Handler Layer]
    end

    subgraph Engine Core
        E --> F[Engine API\nRunJob / Cancel / Stream]
        F --> G[DAG Scheduler\nStep Execution]
        G --> H[Provider Registry]
        G --> I[Checkpoint Manager]
        G --> J[Job Store Adapter]
    end

    subgraph Providers
        H --> H1[OpenAI Provider]
        H --> H2[Ollama Provider]
        H --> H3[Image Generator]
        H --> H4[Local Tool]
    end

    subgraph Persistence
        J --> K[MemoryStore\ninternal/store]
        I --> K
        J --> L[(Future DB)]
    end

    Clients -->|HTTP/SDK| D
    E -->|Streaming events| Clients
    H1 -. remote API .-> M[(OpenAI API)]
    H2 -. local host .-> N[(Ollama Daemon)]
    H3 -. future API .-> O[(Image Service)]
    H4 -. shell tools .-> P[(Local Commands)]
```

- **Clients**: CLI、Web UI、SDK などが HTTP サーバーを介してジョブを発行 / 監視。
- **HTTP Interface**: `internal/server` の handler が REST/NDJSON API を提供し、Engine Core に処理を委譲。
- **Engine Core**: DAG スケジューラが StepDefinition に従って実行し、ProviderRegistry で選択したプロバイダに処理を依頼。StepCheckpoint や JobStore を介して状態を保持。
- **Providers**: OpenAI/Ollama/画像生成/ローカルツールなどに抽象化された呼び出しポイント。実際の外部 API またはローカルコマンドへ接続。
- **Persistence**: 現在はインメモリストア（MemoryStore）、将来は永続 DB へ差し替え予定。

この図をベースに、ドキュメントや README からリンクすることでシステム全体の把握が容易になります。
