import http from "node:http";
import { spawn } from "node:child_process";

const server = http.createServer((req, res) => {
  if (req.url === "/health") {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ status: "ok" }));
    return;
  }
  if (req.url?.startsWith("/v1/jobs")) {
    if (req.method === "POST" && req.url.includes("stream=true")) {
      res.setHeader("Content-Type", "application/x-ndjson");
      const events = [
        { event: "job_queued", job_id: "mock", data: { id: "mock" } },
        { event: "stream_finished", job_id: "mock", data: { status: "succeeded" } }
      ];
      for (const evt of events) {
        res.write(JSON.stringify(evt) + "\n");
      }
      res.end();
      return;
    }
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ job: { id: "mock", pipeline_type: "demo", status: "queued" } }));
    return;
  }
  res.statusCode = 404;
  res.end();
});

server.listen(0, () => {
  const addr = `http://127.0.0.1:${server.address().port}`;
  const child = spawn("npx", ["tsx", "index.ts"], {
    env: { ...process.env, PIPELINE_ENGINE_ADDR: addr },
    stdio: "pipe"
  });
  let output = "";
  child.stdout.on("data", (chunk) => (output += chunk.toString()));
  child.stderr.on("data", (chunk) => (output += chunk.toString()));
  child.on("exit", (code) => {
    server.close();
    if (code !== 0) {
      console.error(output);
      process.exit(code ?? 1);
    }
    if (!output.includes("Accepted job")) {
      console.error("sample output missing expected text");
      process.exit(1);
    }
    process.exit(0);
  });
});
