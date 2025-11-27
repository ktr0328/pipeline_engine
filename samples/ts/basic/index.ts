import { PipelineEngineClient, JobRequest } from "@pipeforge/sdk";

const baseUrl = process.env.PIPELINE_ENGINE_ADDR ?? "http://127.0.0.1:8085";
const client = new PipelineEngineClient({ baseUrl });

async function main() {
  const req: JobRequest = {
    pipeline_type: "openai.summarize.v1",
    input: {
      sources: [
        { kind: "note", content: "CLI から要約したい文章をここに入れてください" }
      ]
    },
    mode: "async"
  };

  console.log("Submitting job to", baseUrl);
  const { job, events } = await client.streamJobs(req);
  console.log("Accepted job:", job.id);
  for await (const evt of events) {
    const kind = evt.event;
    console.log(`[${kind}]`, evt.data);
    if (kind === "stream_finished") {
      break;
    }
  }
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
