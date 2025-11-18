# @pipeforge/engine

Electron / Node.js アプリから Pipeline Engine バイナリを起動・停止するための簡易ヘルパーです。`EngineProcess` は `binaryPath` を指定しない場合、同梱ディレクトリ `bin/` か `PIPELINE_ENGINE_BIN_PATH` 環境変数を参照してバイナリを探します。

## バイナリ配置 / ダウンロード

`postinstall` は GitHub Releases (`v${package.json version}`) からプラットフォーム / アーキテクチャに応じた `pipeline-engine-<platform>-<arch>` を自動でダウンロードし、`bin/` に格納します。まだリリースされていないバージョンや独自ビルドを利用したい場合は、以下の方法で上書きできます。

1. **既存バイナリをコピー**  
   - `PIPELINE_ENGINE_ENGINE_SOURCE=/absolute/path/to/pipeline-engine` をセットした状態で `npm install` すると、postinstall が `bin/` へコピーします。
2. **URL / テンプレート指定**  
   - `PIPELINE_ENGINE_ENGINE_DOWNLOAD_URL` で直接 URL を指定するか、`PIPELINE_ENGINE_ENGINE_DOWNLOAD_URL_TEMPLATE=https://example.com/{{version}}/pipeline-engine-{{platform}}-{{arch}}{{ext}}` のようにテンプレートを設定します。`{{version}}` は `PIPELINE_ENGINE_ENGINE_VERSION`（既定は package.json の `version`）が使われます。
3. **手動配置**  
   - 任意の場所にバイナリを配置し、`PIPELINE_ENGINE_BIN_PATH` で絶対パスを渡すか `EngineProcess` の `binaryPath` を指定します。

> `npm run engine:download` は `PIPELINE_ENGINE_ENGINE_SOURCE` / `PIPELINE_ENGINE_ENGINE_DOWNLOAD_URL[_TEMPLATE]` が未設定の場合にエラーを返すため、CI などでバイナリ取得を強制したいときに利用してください。

## 使い方

```ts
import { EngineProcess } from "@pipeforge/engine";
import { PipelineEngineClient } from "@pipeforge/sdk";

const engine = new EngineProcess({
  env: {
    PIPELINE_ENGINE_OPENAI_API_KEY: process.env.OPENAI_API_KEY,
    PIPELINE_ENGINE_ADDR: "127.0.0.1:8085"
  }
});
await engine.start();

const client = new PipelineEngineClient({ baseUrl: "http://127.0.0.1:8085" });
const { events } = await client.streamJobs({ pipeline_type: "openai.funmarkdown.v1", input: { sources: [] } });
for await (const evt of events) {
  console.log(evt);
}

await engine.stop();
```

`start()` は `/health` の応答を待ち合わせてから resolve します。`stop()` は `SIGTERM` でプロセスを終了させます。

## 注意
-	バイナリは含まれていないため、各プラットフォーム向けにビルドしたものを同梱するか、postinstall で取得してください。
-	Electron で利用する場合はメインプロセスで `EngineProcess` を管理し、レンダラーからは IPC 経由で SDK を呼び出す構成が推奨です。
