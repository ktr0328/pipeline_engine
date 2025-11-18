# SDK Design Notes

Pipeline Engine exposes a simple HTTP/NDJSON API, so SDKs just need thin wrappers over `/health`, `/v1/jobs`, `/v1/jobs/{id}`, `/v1/jobs/{id}/stream`, `/v1/jobs/{id}/cancel`, `/v1/jobs/{id}/rerun`.

## TypeScript / Node SDK

### Goals
- Provide promise based wrappers (`createJob`, `createJobStream`, `getJob`, `cancelJob`, `rerunJob`).
- Include helper `JobStream` class that emits typed events via `EventEmitter` (maps to JSON messages).
- Support both Node (Fetch) and browser/Electron contexts (configurable baseURL, fetch implementation).

### Structure
```
src/
  client.ts       // exported PipelineEngineClient class
  stream.ts       // JobStream helper that handles EventSource or fetch streaming
  types.ts        // Type declarations mirroring engine.StepDef etc.
```

### Usage sketch
```ts
const client = new PipelineEngineClient({ baseURL: 'http://127.0.0.1:8085' });
const job = await client.createJob({ pipeline_type: 'summarize.v0', input: {...} });
const stream = await client.createJobStream({ pipeline_type: 'summarize.v0', input: {...} });
stream.on('item_completed', evt => console.log(evt.data));
await stream.untilDone();
```

### Implementation notes
- Use native `fetch` or `node-fetch`. Provide `RequestInit` override for headers (API keys, custom host).
- Streaming: use `ReadableStream` reader (browser) or `node:stream` to parse NDJSON.
- Publish to npm (`@pipeline-engine/sdk`). Provide TypeScript types.

## Python SDK

### Goals
- Provide synchronous + async client (requests + httpx).
- Support context manager for streaming generator: `for event in client.stream_job(req): ...`.

### Structure
```
pipeline_sdk/
  __init__.py
  client.py        # PipelineEngineClient class
  aio.py           # AsyncPipelineEngineClient using httpx.AsyncClient
  models.py        # dataclasses for Job, JobRequest, StreamingEvent
```

### Usage sketch
```python
client = PipelineEngineClient(base_url="http://127.0.0.1:8085")
job = client.create_job(JobRequest(...))
for event in client.stream_job(JobRequest(...)):
    print(event.event, event.data)
```

### Implementation notes
- Use `pydantic` or `dataclasses` for type safety.
- Provide `timeout` + retry hooks.
- Publish to PyPI (`pipeline-engine-sdk`).

## Shared Considerations
- Both SDKs should expose ProviderProfile helpers later (load from config file).
- Provide CLI snippets + README linking to this document for future work.
