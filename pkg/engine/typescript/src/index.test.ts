import test from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { EngineProcess, resolveBinaryPath } from "./index.js";

function createTempScript(contents: string) {
  const dir = mkdtempSync(path.join(os.tmpdir(), "engine-test-"));
  const scriptPath = path.join(dir, "fake-engine.mjs");
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

test("resolveBinaryPath respects env override", () => {
  const custom = path.join(os.tmpdir(), "pipeline-engine-custom");
  process.env.PIPELINE_ENGINE_BIN_PATH = custom;
  try {
    assert.equal(resolveBinaryPath(), custom);
  } finally {
    delete process.env.PIPELINE_ENGINE_BIN_PATH;
  }
});

test("EngineProcess waits for health and stops", async (t) => {
  const port = await reservePort();
  const { scriptPath, cleanup } = createTempScript(`#!/usr/bin/env node
import http from "node:http";
const port = Number(process.env.TEST_HEALTH_PORT);
const server = http.createServer((req, res) => {
  if (req.url === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "ok" }));
    return;
  }
  res.writeHead(404);
  res.end();
});
server.listen(port, "127.0.0.1");
process.on("SIGTERM", () => {
  server.close(() => process.exit(0));
});
`);
  t.after(cleanup);
  const engine = new EngineProcess({
    binaryPath: scriptPath,
    env: { TEST_HEALTH_PORT: String(port) },
    healthUrl: `http://127.0.0.1:${port}/health`,
    readyTimeoutMs: 2_000
  });
  await engine.start();
  const resp = await fetch(`http://127.0.0.1:${port}/health`);
  assert.equal(resp.status, 200);
  await engine.stop();
});
