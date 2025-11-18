import test from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { EngineProcess } from "../src/index.js";
import { PipelineEngineClient } from "../../../sdk/typescript/src/client.js";
import type { JobRequest } from "../../../sdk/typescript/src/types.js";

function createFakeEngineScript() {
  const dir = mkdtempSync(path.join(os.tmpdir(), "engine-integration-"));
  const scriptPath = path.join(dir, "fake-engine.mjs");
  const contents = `#!/usr/bin/env node
import http from "node:http";
const port = Number(process.env.TEST_ENGINE_PORT);
const job = {
  id: "job-demo",
  pipeline_type: "demo.pipeline",
  status: "queued",
  input: { sources: [] }
};
const events = [
  { event: "job_queued", job_id: job.id, data: job },
  { event: "provider_chunk", job_id: job.id, data: { step_id: "step-1", index: 0, content: "chunk-1" } },
  { event: "step_started", job_id: job.id, data: { step_id: "step-1", status: "running" } },
  { event: "step_completed", job_id: job.id, data: { step_id: "step-1", status: "success" } },
  { event: "job_completed", job_id: job.id, data: { status: "succeeded", result: { items: [] } } },
  { event: "stream_finished", job_id: job.id, data: {} }
];
const server = http.createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://127.0.0.1");
  if (req.method === "GET" && url.pathname === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "ok" }));
    return;
  }
  if (req.method === "POST" && url.pathname === "/v1/config/providers") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ ok: true }));
    return;
  }
  if (req.method === "POST" && url.pathname === "/v1/jobs" && url.searchParams.get("stream") === "true") {
    res.writeHead(200, { "Content-Type": "application/x-ndjson" });
    let idx = 0;
    const writeNext = () => {
      if (idx >= events.length) {
        res.end();
        return;
      }
      const payload = JSON.stringify(events[idx]) + "\\n";
      res.write(payload);
      idx += 1;
      setTimeout(writeNext, 10);
    };
    writeNext();
    return;
  }
  res.writeHead(404);
  res.end();
});
server.listen(port, "127.0.0.1");
process.on("SIGTERM", () => {
  server.close(() => process.exit(0));
});
`;
  writeFileSync(scriptPath, contents, "utf8");
  chmodSync(scriptPath, 0o755);
  return { scriptPath, cleanup: () => rmSync(dir, { recursive: true, force: true }) };
}

function reservePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = http.createServer();
    srv.listen(0, () => {
      const addr = srv.address();
      srv.close(() => {
        if (typeof addr === "object" && addr) {
          resolve(addr.port);
        } else {
          reject(new Error("unexpected address"));
        }
      });
    });
    srv.on("error", reject);
  });
}

test("EngineProcess + PipelineEngineClient stream end-to-end", async (t) => {
  const { scriptPath, cleanup } = createFakeEngineScript();
  t.after(cleanup);
  const port = await reservePort();
  const engine = new EngineProcess({
    binaryPath: scriptPath,
    env: { TEST_ENGINE_PORT: String(port) },
    healthUrl: `http://127.0.0.1:${port}/health`,
    readyTimeoutMs: 5_000
  });
  await engine.start();
  t.after(() => engine.stop());
  const client = new PipelineEngineClient({ baseUrl: `http://127.0.0.1:${port}` });
  await client.upsertProviderProfile({ id: "local", kind: "local" });
  const req: JobRequest = { pipeline_type: "demo.pipeline", input: { sources: [{ kind: "note", content: "hello" }] } };
  const { job, events } = await client.streamJobs(req);
  assert.equal(job.id, "job-demo");
  const seen: string[] = [];
  for await (const evt of events) {
    seen.push(evt.event);
  }
  assert.deepEqual(seen, ["provider_chunk", "step_started", "step_completed", "job_completed", "stream_finished"]);
});
