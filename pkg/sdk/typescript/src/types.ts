export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";
export type StepExecutionStatus = "pending" | "running" | "success" | "failed" | "skipped" | "cancelled";

export interface Source {
  kind: string;
  label?: string;
  content: string;
}

export interface JobInput {
  sources: Source[];
  options?: Record<string, unknown>;
}

export interface JobRequest {
  pipeline_type: string;
  input: JobInput;
  mode?: string;
  parent_job_id?: string;
  from_step_id?: string;
  reuse_upstream?: boolean;
}

export interface StepExecution {
  step_id: string;
  status: StepExecutionStatus;
  chunks?: StepChunk[];
  error?: JobError;
}

export interface StepChunk {
  step_id: string;
  index: number;
  content: string;
}

export interface ProviderProfileInput {
  id: string;
  kind: string;
  base_uri?: string;
  api_key?: string;
  default_model?: string;
  extra?: Record<string, unknown>;
}

export interface EngineConfigInput {
  log_level?: string;
}

export interface Job {
  id: string;
  pipeline_type: string;
  pipeline_version?: string;
  status: JobStatus;
  input: JobInput;
  result?: JobResult;
  step_executions?: StepExecution[];
  error?: JobError;
}

export interface JobResult {
  items: ResultItem[];
}

export interface ResultItem {
  id: string;
  label: string;
  step_id: string;
  kind: string;
  content_type: string;
  data: Record<string, unknown>;
}

export interface JobError {
  code: string;
  message: string;
  details?: unknown;
}

export interface StreamingEvent<T = any> {
  seq?: number;
  event: string;
  job_id: string;
  data: T;
}

export type FetchLike = typeof fetch;
