import { createInterface } from "node:readline";
import type { Readable, Writable } from "node:stream";
import type { PipelineEngineClient } from "../client.js";
import type {
  Job,
  JobRequest,
  ProviderProfileInput,
  StreamingEvent
} from "../types.js";

const JSONRPC_VERSION = "2.0";

interface RPCRequest {
  jsonrpc?: string;
  id?: number | string | null;
  method: string;
  params?: any;
}

interface ToolCallParams {
  toolName: string;
  arguments?: any;
}

export interface MCPClient {
  createJob(req: JobRequest): Promise<Job>;
  streamJobs(req: JobRequest): Promise<{ job: Job; events: AsyncIterable<StreamingEvent> }>;
  getJob(jobID: string): Promise<Job>;
  cancelJob(jobID: string, reason?: string): Promise<Job>;
  rerunJob(jobID: string, payload: Record<string, unknown>): Promise<Job>;
  upsertProviderProfile(profile: ProviderProfileInput): Promise<void>;
  streamJobByID(jobID: string, afterSeq?: number): Promise<AsyncIterable<StreamingEvent>>;
}

export interface ToolDefinition {
  name: string;
  description: string;
  inputSchema?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface MCPAdapterOptions {
  client: MCPClient;
  input?: Readable;
  output?: Writable;
  tools?: ToolDefinition[];
  logger?: { error: (...args: unknown[]) => void };
}

export class MCPAdapter {
  private readonly client: MCPClient;
  private readonly input: Readable;
  private readonly output: Writable;
  private readonly tools: ToolDefinition[];
  private readonly logger?: { error: (...args: unknown[]) => void };

  constructor(options: MCPAdapterOptions) {
    if (!options?.client) {
      throw new Error("client is required");
    }
    this.client = options.client;
    this.input = options.input ?? (process.stdin as Readable);
    this.output = options.output ?? (process.stdout as Writable);
    this.tools = options.tools ?? defaultTools();
    this.logger = options.logger;
  }

  async run(): Promise<void> {
    const rl = createInterface({ input: this.input, crlfDelay: Infinity });
    try {
      for await (const rawLine of rl) {
        const line = rawLine.trim();
        if (!line) {
          continue;
        }
        let req: RPCRequest;
        try {
          req = JSON.parse(line) as RPCRequest;
        } catch (err) {
          this.respondError(null, -32700, "parse error", String(err));
          continue;
        }
        await this.handleRequest(req).catch((err) => {
          this.logger?.error?.("adapter error", err);
          this.respondError(req.id ?? null, -32603, "internal error", err instanceof Error ? err.message : String(err));
        });
      }
    } finally {
      rl.close();
    }
  }

  private async handleRequest(req: RPCRequest): Promise<void> {
    if (req.jsonrpc && req.jsonrpc !== JSONRPC_VERSION) {
      this.respondError(req.id ?? null, -32600, "invalid jsonrpc version", null);
      return;
    }
    if (!req.method) {
      this.respondError(req.id ?? null, -32600, "method is required", null);
      return;
    }
    switch (req.method) {
      case "initialize":
        this.respondResult(req.id, {
          protocolVersion: "0.1",
          serverInfo: { name: "pipeline-engine-mcp-ts", version: "0.1.0" },
          capabilities: {
            tools: { list: true, call: true },
            prompts: { list: false },
            resources: { list: false },
            experimental: {}
          }
        });
        break;
      case "tools/list":
        this.respondResult(req.id, { tools: this.tools });
        break;
      case "tools/call":
        await this.handleToolCall(req);
        break;
      default:
        this.respondError(req.id ?? null, -32601, "method not found", req.method);
    }
  }

  private async handleToolCall(req: RPCRequest): Promise<void> {
    if (!req.params) {
      this.respondError(req.id ?? null, -32602, "missing params", null);
      return;
    }
    const params = req.params as ToolCallParams;
    if (!params.toolName) {
      this.respondError(req.id ?? null, -32602, "toolName is required", null);
      return;
    }
    switch (params.toolName) {
      case "startPipeline":
        await this.handleStartPipeline(req.id, params.arguments ?? {});
        break;
      case "getJob":
        await this.handleGetJob(req.id, params.arguments ?? {});
        break;
      case "cancelJob":
        await this.handleCancelJob(req.id, params.arguments ?? {});
        break;
      case "rerunJob":
        await this.handleRerunJob(req.id, params.arguments ?? {});
        break;
      case "upsertProviderProfile":
        await this.handleUpsertProviderProfile(req.id, params.arguments ?? {});
        break;
      case "streamJob":
        await this.handleStreamJob(req.id, params.arguments ?? {});
        break;
      default:
        this.respondError(req.id ?? null, -32601, "tool not found", params.toolName);
    }
  }

  private async handleStartPipeline(id: RPCRequest["id"], args: any): Promise<void> {
    if (!args?.pipeline_type || !args?.input) {
      this.respondError(id ?? null, -32602, "pipeline_type and input are required", null);
      return;
    }
    const req: JobRequest = {
      pipeline_type: args.pipeline_type,
      input: args.input,
      mode: args.mode,
      from_step_id: args.from_step_id,
      reuse_upstream: args.reuse_upstream
    };
    if (args.stream) {
      const { job, events } = await this.client.streamJobs(req);
      this.emitToolEvent("startPipeline", { event: "job_queued", job_id: job.id, data: job });
      for await (const evt of events) {
        this.emitToolEvent("startPipeline", evt);
      }
      this.respondResult(id, { job });
      return;
    }
    const job = await this.client.createJob(req);
    this.respondResult(id, { job });
  }

  private async handleGetJob(id: RPCRequest["id"], args: any): Promise<void> {
    if (!args?.job_id) {
      this.respondError(id ?? null, -32602, "job_id is required", null);
      return;
    }
    const job = await this.client.getJob(args.job_id);
    this.respondResult(id, { job });
  }

  private async handleCancelJob(id: RPCRequest["id"], args: any): Promise<void> {
    if (!args?.job_id) {
      this.respondError(id ?? null, -32602, "job_id is required", null);
      return;
    }
    const job = await this.client.cancelJob(args.job_id, args.reason);
    this.respondResult(id, { job });
  }

  private async handleRerunJob(id: RPCRequest["id"], args: any): Promise<void> {
    if (!args?.job_id) {
      this.respondError(id ?? null, -32602, "job_id is required", null);
      return;
    }
    const payload: Record<string, unknown> = {};
    if (args.from_step_id) {
      payload.from_step_id = args.from_step_id;
    }
    if (typeof args.reuse_upstream === "boolean") {
      payload.reuse_upstream = args.reuse_upstream;
    }
    if (args.override_input) {
      payload.override_input = args.override_input;
    }
    const job = await this.client.rerunJob(args.job_id, payload);
    this.respondResult(id, { job });
  }

  private async handleUpsertProviderProfile(id: RPCRequest["id"], args: any): Promise<void> {
    if (!args?.id || !args?.kind) {
      this.respondError(id ?? null, -32602, "id and kind are required", null);
      return;
    }
    await this.client.upsertProviderProfile(args as ProviderProfileInput);
    this.respondResult(id, { profile: args });
  }

  private async handleStreamJob(id: RPCRequest["id"], args: any): Promise<void> {
    if (!args?.job_id) {
      this.respondError(id ?? null, -32602, "job_id is required", null);
      return;
    }
    const afterSeq =
      typeof args.after_seq === "number" && args.after_seq > 0 ? args.after_seq : undefined;
    const events = await this.client.streamJobByID(args.job_id, afterSeq);
    for await (const evt of events) {
      this.emitToolEvent("streamJob", evt);
    }
    this.respondResult(id, { job_id: args.job_id });
  }

  private respondResult(id: RPCRequest["id"], result: any): void {
    if (id === undefined || id === null) {
      return;
    }
    this.writeJSON({ jsonrpc: JSONRPC_VERSION, id, result });
  }

  private respondError(id: RPCRequest["id"], code: number, message: string, data: unknown): void {
    if (id === undefined || id === null) {
      return;
    }
    this.writeJSON({ jsonrpc: JSONRPC_VERSION, id, error: { code, message, data } });
  }

  private emitToolEvent(toolName: string, evt: StreamingEvent): void {
    const notification = {
      jsonrpc: JSONRPC_VERSION,
      method: "tool_event",
      params: {
        toolName,
        event: evt.event,
        kind: classifyEventKind(evt.event),
        seq: evt.seq,
        payload: evt
      }
    };
    this.writeJSON(notification);
  }

  private writeJSON(payload: unknown): void {
    try {
      this.output.write(`${JSON.stringify(payload)}\n`);
    } catch (err) {
      this.logger?.error?.("failed to write json", err);
    }
  }
}

function classifyEventKind(eventName: string): string {
  switch (eventName) {
    case "provider_chunk":
      return "chunk";
    case "item_completed":
      return "result";
    case "error":
    case "job_failed":
    case "step_failed":
      return "error";
    default:
      return "status";
  }
}

function defaultTools(): ToolDefinition[] {
  return [
    {
      name: "startPipeline",
      description: "Create a job via /v1/jobs",
      inputSchema: {
        type: "object",
        required: ["pipeline_type", "input"],
        properties: {
          pipeline_type: { type: "string" },
          input: { type: "object" },
          mode: { type: "string" },
          stream: { type: "boolean" }
        }
      }
    },
    {
      name: "streamJob",
      description: "Stream events for an existing job",
      inputSchema: {
        type: "object",
        required: ["job_id"],
        properties: {
          job_id: { type: "string" },
          after_seq: { type: "number", minimum: 0 }
        }
      }
    },
    {
      name: "getJob",
      description: "Fetch job details",
      inputSchema: {
        type: "object",
        required: ["job_id"],
        properties: { job_id: { type: "string" } }
      }
    },
    {
      name: "cancelJob",
      description: "Cancel a running job",
      inputSchema: {
        type: "object",
        required: ["job_id"],
        properties: {
          job_id: { type: "string" },
          reason: { type: "string" }
        }
      }
    },
    {
      name: "rerunJob",
      description: "Rerun a job from a specific step",
      inputSchema: {
        type: "object",
        required: ["job_id"],
        properties: {
          job_id: { type: "string" },
          from_step_id: { type: "string" },
          reuse_upstream: { type: "boolean" },
          override_input: { type: "object" }
        }
      }
    },
    {
      name: "upsertProviderProfile",
      description: "Upsert a provider profile",
      inputSchema: {
        type: "object",
        required: ["id", "kind"],
        properties: {
          id: { type: "string" },
          kind: { type: "string" },
          base_uri: { type: "string" },
          api_key: { type: "string" },
          default_model: { type: "string" }
        }
      }
    }
  ];
}

export function createMCPAdapterClient(client: PipelineEngineClient): MCPClient {
  return {
    createJob: (req) => client.createJob(req),
    streamJobs: (req) => client.streamJobs(req),
    getJob: (id) => client.getJob(id),
    cancelJob: (id, reason) => client.cancelJob(id, reason),
    rerunJob: (id, payload) => client.rerunJob(id, payload),
    upsertProviderProfile: (profile) => client.upsertProviderProfile(profile),
    streamJobByID: (id, afterSeq) => client.streamJobByID(id, afterSeq)
  };
}
