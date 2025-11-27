import {
  EngineConfigInput,
  FetchLike,
  Job,
  JobRequest,
  ProviderProfileInput,
  StreamingEvent
} from "./types.js";

export interface PipelineEngineClientOptions {
  baseUrl: string;
  fetch?: FetchLike;
}

export interface StreamJobsResult {
  job: Job;
  events: AsyncIterable<StreamingEvent>;
}

export class PipelineEngineClient {
  private readonly baseUrl: string;
  private readonly fetchImpl: FetchLike;

  constructor(options: PipelineEngineClientOptions) {
    if (!options || !options.baseUrl) {
      throw new Error("baseUrl is required");
    }
    this.baseUrl = options.baseUrl.replace(/\/$/, "");
    this.fetchImpl = options.fetch ?? globalThis.fetch;
    if (!this.fetchImpl) {
      throw new Error("fetch API is not available. Provide options.fetch or run on Node 18+");
    }
  }

  async createJob(req: JobRequest): Promise<Job> {
    return this.postJob("/v1/jobs", req, 202);
  }

  async getJob(jobID: string): Promise<Job> {
    return this.requestJSON(`/v1/jobs/${jobID}`, { method: "GET" });
  }

  async cancelJob(jobID: string, reason = "user"): Promise<Job> {
    return this.requestJSON(`/v1/jobs/${jobID}/cancel`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ reason })
    });
  }

  async rerunJob(jobID: string, payload: Record<string, unknown>): Promise<Job> {
    return this.requestJSON(`/v1/jobs/${jobID}/rerun`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });
  }

  async upsertProviderProfile(profile: ProviderProfileInput): Promise<void> {
    const resp = await this.fetchImpl(`${this.baseUrl}/v1/config/providers`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(profile)
    });
    if (!resp.ok) {
      throw new Error(`http error: ${resp.status} ${resp.statusText}`);
    }
  }

  async updateEngineConfig(config: EngineConfigInput): Promise<Record<string, unknown>> {
    const resp = await this.fetchImpl(`${this.baseUrl}/v1/config/engine`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(config)
    });
    if (!resp.ok) {
      throw new Error(`http error: ${resp.status} ${resp.statusText}`);
    }
    return resp.json();
  }

  async streamJobs(req: JobRequest): Promise<StreamJobsResult> {
    const resp = await this.fetchImpl(`${this.baseUrl}/v1/jobs?stream=true`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req)
    });
    if (!resp.ok || !resp.body) {
      throw new Error(`http error: ${resp.status} ${resp.statusText}`);
    }
    const iterator = this.ndjsonIterator(resp.body);
    const first = await iterator.next();
    if (first.done) {
      throw new Error("stream closed before job event");
    }
    const jobEvent = first.value;
    if (jobEvent.event !== "job_queued") {
      throw new Error(`unexpected first event: ${jobEvent.event}`);
    }
    const job = jobEvent.data as Job;
    const events = (async function* () {
      for await (const evt of iterator) {
        yield evt;
      }
    })();
    return { job, events };
  }

  async streamJobByID(jobID: string): Promise<AsyncIterable<StreamingEvent>> {
    const resp = await this.fetchImpl(`${this.baseUrl}/v1/jobs/${jobID}/stream`, {
      method: "GET"
    });
    if (!resp.ok || !resp.body) {
      throw new Error(`http error: ${resp.status} ${resp.statusText}`);
    }
    return this.ndjsonIterator(resp.body);
  }

  private async postJob(path: string, req: JobRequest, expectedStatus: number): Promise<Job> {
    const resp = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(req)
    });
    if (resp.status !== expectedStatus) {
      throw new Error(`http error: ${resp.status} ${resp.statusText}`);
    }
    return this.decodeJob(await resp.json());
  }

  private async requestJSON(path: string, init: RequestInit): Promise<Job> {
    const resp = await this.fetchImpl(`${this.baseUrl}${path}`, init);
    if (!resp.ok) {
      throw new Error(`http error: ${resp.status} ${resp.statusText}`);
    }
    return this.decodeJob(await resp.json());
  }

  private decodeJob(payload: any): Job {
    if (payload.job) {
      return payload.job as Job;
    }
    return payload as Job;
  }

  private ndjsonIterator(stream: ReadableStream<Uint8Array>): AsyncGenerator<StreamingEvent> {
    const reader = stream.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    return (async function* () {
      try {
        while (true) {
          const { value, done } = await reader.read();
          if (done) {
            break;
          }
          buffer += decoder.decode(value, { stream: true });
          let newlineIndex;
          while ((newlineIndex = buffer.indexOf("\n")) >= 0) {
            const line = buffer.slice(0, newlineIndex).trim();
            buffer = buffer.slice(newlineIndex + 1);
            if (!line) {
              continue;
            }
            yield JSON.parse(line) as StreamingEvent;
          }
        }
        const tail = decoder.decode();
        if (tail) {
          buffer += tail;
        }
        if (buffer.trim()) {
          yield JSON.parse(buffer.trim()) as StreamingEvent;
        }
      } finally {
        await reader.cancel().catch(() => void 0);
      }
    })();
  }
}
