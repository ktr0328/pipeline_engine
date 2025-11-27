import test from "node:test";
import assert from "node:assert/strict";
import { Readable } from "node:stream";
import { Writable } from "node:stream";
import { MCPAdapter } from "./adapter.js";
import type {
  Job,
  JobRequest,
  ProviderProfileInput,
  StreamingEvent
} from "../types.js";

class StubClient {
  job: Job = { id: "job-1", pipeline_type: "demo", status: "queued", input: { sources: [] } };
  streamedEvents: StreamingEvent[] = [];
  lastAfterSeq?: number;

  async createJob(req: JobRequest): Promise<Job> {
    this.job = { ...this.job, pipeline_type: req.pipeline_type };
    return this.job;
  }

  async streamJobs(): Promise<{ job: Job; events: AsyncIterable<StreamingEvent> }> {
    const events = this.streamedEvents;
    return {
      job: this.job,
      events: (async function* () {
        for (const evt of events) {
          yield evt;
        }
      })()
    };
  }

  async getJob(): Promise<Job> {
    return this.job;
  }

  async cancelJob(): Promise<Job> {
    return this.job;
  }

  async rerunJob(): Promise<Job> {
    return this.job;
  }

  async upsertProviderProfile(_profile: ProviderProfileInput): Promise<void> {
    return;
  }

  async streamJobByID(_jobID: string, afterSeq?: number): Promise<AsyncIterable<StreamingEvent>> {
    this.lastAfterSeq = afterSeq;
    const events = this.streamedEvents;
    return (async function* () {
      for (const evt of events) {
        yield evt;
      }
    })();
  }
}

function makeStreams(payload: object): { input: Readable; output: Writable; chunks: string[] } {
  const input = Readable.from([JSON.stringify(payload), "\n"]);
  const chunks: string[] = [];
  const output = new Writable({
    write(chunk, _enc, cb) {
      chunks.push(chunk.toString());
      cb();
    }
  });
  return { input, output, chunks };
}

function parseOutputs(chunks: string[]): any[] {
  return chunks
    .join("")
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line));
}

test("adapter responds to tools/list", async () => {
  const client = new StubClient();
  const streams = makeStreams({ jsonrpc: "2.0", id: 1, method: "tools/list", params: {} });
  const adapter = new MCPAdapter({ client, input: streams.input, output: streams.output });
  await adapter.run();
  const responses = parseOutputs(streams.chunks);
  assert.equal(responses[0].result.tools.length > 0, true);
});

test("startPipeline stream emits tool events", async () => {
  const client = new StubClient();
  client.streamedEvents = [
    { event: "provider_chunk", job_id: "job-1", data: { content: "chunk" } },
    { event: "item_completed", job_id: "job-1", data: { label: "default" } }
  ];
  const streams = makeStreams({
    jsonrpc: "2.0",
    id: 2,
    method: "tools/call",
    params: {
      toolName: "startPipeline",
      arguments: { pipeline_type: "demo", input: { sources: [] }, stream: true }
    }
  });
  const adapter = new MCPAdapter({ client, input: streams.input, output: streams.output });
  await adapter.run();
  const responses = parseOutputs(streams.chunks);
  const toolEvents = responses.filter((msg) => msg.method === "tool_event");
  const result = responses.find((msg) => msg.result);
  assert.equal(toolEvents.length, 3); // job_queued + 2 events
  const kinds = toolEvents.map((evt) => evt.params?.kind);
  assert.deepEqual(kinds, ["status", "chunk", "result"]);
  assert.ok(result?.result?.job);
});

test("streamJob forwards events", async () => {
  const client = new StubClient();
  client.streamedEvents = [
    { event: "provider_chunk", job_id: "job-1", data: { content: "chunk" } }
  ];
  const streams = makeStreams({
    jsonrpc: "2.0",
    id: 3,
    method: "tools/call",
    params: { toolName: "streamJob", arguments: { job_id: "job-1" } }
  });
  const adapter = new MCPAdapter({ client, input: streams.input, output: streams.output });
  await adapter.run();
  const responses = parseOutputs(streams.chunks);
  const toolEvents = responses.filter((msg) => msg.method === "tool_event");
  assert.equal(toolEvents.length, 1);
  assert.equal(toolEvents[0]?.params?.kind, "chunk");
  const result = responses.find((msg) => msg.result);
  assert.equal(result?.result?.job_id, "job-1");
});

test("streamJob accepts after_seq argument", async () => {
  const client = new StubClient();
  client.streamedEvents = [
    { event: "job_status", job_id: "job-2", data: { status: "running" } }
  ];
  const streams = makeStreams({
    jsonrpc: "2.0",
    id: 4,
    method: "tools/call",
    params: { toolName: "streamJob", arguments: { job_id: "job-2", after_seq: 9 } }
  });
  const adapter = new MCPAdapter({ client, input: streams.input, output: streams.output });
  await adapter.run();
  assert.equal(client.lastAfterSeq, 9);
});
