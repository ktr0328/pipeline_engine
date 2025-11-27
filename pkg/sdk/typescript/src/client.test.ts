import test from "node:test";
import assert from "node:assert/strict";
import { PipelineEngineClient } from "./client.js";
import type { FetchLike, JobRequest, StreamingEvent } from "./types.js";

const encoder = new TextEncoder();

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" }
  });
}

function ndjsonResponse(events: StreamingEvent[]): Response {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const evt of events) {
        controller.enqueue(encoder.encode(`${JSON.stringify(evt)}\n`));
      }
      controller.close();
    }
  });
  return new Response(stream, {
    status: 200,
    headers: { "Content-Type": "application/x-ndjson" }
  });
}

test("createJob unwraps job payload", async () => {
  const fetchMock: FetchLike = async () => jsonResponse({ job: { id: "job-1", pipeline_type: "demo", status: "queued", input: { sources: [] } } }, 202);
  const client = new PipelineEngineClient({ baseUrl: "http://localhost:9000", fetch: fetchMock });
  const req: JobRequest = { pipeline_type: "demo", input: { sources: [] } };
  const job = await client.createJob(req);
  assert.equal(job.id, "job-1");
  assert.equal(job.status, "queued");
});

test("upsertProviderProfile posts JSON body", async () => {
  let capturedUrl = "";
  let capturedBody = "";
  const fetchMock: FetchLike = async (url, init) => {
    capturedUrl = url.toString();
    capturedBody = init?.body?.toString() ?? "";
    return new Response(null, { status: 200 });
  };
  const client = new PipelineEngineClient({ baseUrl: "http://127.0.0.1:8085", fetch: fetchMock });
  await client.upsertProviderProfile({ id: "local", kind: "local" });
  assert.equal(capturedUrl, "http://127.0.0.1:8085/v1/config/providers");
  assert.equal(capturedBody, JSON.stringify({ id: "local", kind: "local" }));
});

test("streamJobs yields events after job_queued", async () => {
  const jobEvent: StreamingEvent = {
    event: "job_queued",
    job_id: "job-2",
    data: { id: "job-2", pipeline_type: "demo", status: "queued", input: { sources: [] } }
  };
  const tailEvents: StreamingEvent[] = [
    { event: "provider_chunk", job_id: "job-2", data: { content: "chunk" } },
    { event: "job_completed", job_id: "job-2", data: { status: "succeeded" } },
    { event: "stream_finished", job_id: "job-2", data: {} }
  ];
  const fetchMock: FetchLike = async () => ndjsonResponse([jobEvent, ...tailEvents]);
  const client = new PipelineEngineClient({ baseUrl: "http://localhost:8085", fetch: fetchMock });
  const req: JobRequest = { pipeline_type: "demo", input: { sources: [] } };
  const { job, events } = await client.streamJobs(req);
  assert.equal(job.id, "job-2");
  const seen: string[] = [];
  for await (const evt of events) {
    seen.push(evt.event);
  }
  assert.deepEqual(seen, tailEvents.map((evt) => evt.event));
});

test("streamJobByID yields historical events", async () => {
  const events: StreamingEvent[] = [
    { event: "job_status", job_id: "job-3", data: { status: "running" } },
    { event: "job_completed", job_id: "job-3", data: { status: "succeeded" } }
  ];
  const fetchMock: FetchLike = async () => ndjsonResponse(events);
  const client = new PipelineEngineClient({ baseUrl: "http://localhost:8085", fetch: fetchMock });
  const stream = await client.streamJobByID("job-3");
  const seen: string[] = [];
  for await (const evt of stream) {
    seen.push(evt.event);
  }
  assert.deepEqual(seen, ["job_status", "job_completed"]);
});
