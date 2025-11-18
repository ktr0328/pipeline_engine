import { ChildProcess, spawn } from "node:child_process";
import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

export interface EngineProcessOptions {
  /** Absolute path to the pipeline-engine binary. Falls back to resolveBinaryPath(). */
  binaryPath?: string;
  /** Additional CLI arguments (e.g., ["-config", "./engine.yaml"]) */
  args?: string[];
  /** Environment variables passed to the engine process. */
  env?: NodeJS.ProcessEnv;
  /** Health check URL (default http://127.0.0.1:8085/health). */
  healthUrl?: string;
  /** Milliseconds to wait for the engine to become healthy. */
  readyTimeoutMs?: number;
}

export interface ResolveBinaryPathOptions {
  /** Name of the environment variable to check (default PIPELINE_ENGINE_BIN_PATH). */
  envVar?: string;
}

type EngineProcessResolvedOptions = EngineProcessOptions & { binaryPath: string };

const DEFAULT_ENV_VAR = "PIPELINE_ENGINE_BIN_PATH";

/**
 * Resolve the pipeline-engine binary path.
 * Order: explicit env var -> packaged bin folder -> undefined.
 */
export function resolveBinaryPath(options?: ResolveBinaryPathOptions): string | undefined {
  const envVar = options?.envVar ?? DEFAULT_ENV_VAR;
  const envPath = process.env[envVar];
  if (envPath) {
    return envPath;
  }
  const binDir = fileURLToPath(new URL("../bin", import.meta.url));
  const candidate = path.join(binDir, process.platform === "win32" ? "pipeline-engine.exe" : "pipeline-engine");
  if (existsSync(candidate)) {
    return candidate;
  }
  return undefined;
}

export class EngineProcess {
  private readonly options: EngineProcessResolvedOptions;
  private child?: ChildProcess;

  constructor(options: EngineProcessOptions) {
    const binaryPath = options?.binaryPath ?? resolveBinaryPath();
    if (!binaryPath) {
      throw new Error("binaryPath is required");
    }
    this.options = { ...options, binaryPath };
  }

  async start(): Promise<void> {
    if (this.child) {
      throw new Error("engine already started");
    }
    const env = { ...process.env, ...this.options.env };
    this.child = spawn(this.options.binaryPath, this.options.args ?? [], {
      env,
      stdio: "inherit"
    });
    const healthUrl = this.options.healthUrl ?? "http://127.0.0.1:8085/health";
    const timeoutMs = this.options.readyTimeoutMs ?? 10_000;
    const startTime = Date.now();
    while (Date.now() - startTime < timeoutMs) {
      if (await this.checkHealth(healthUrl)) {
        return;
      }
      await delay(300);
    }
    throw new Error(`engine did not become healthy within ${timeoutMs}ms`);
  }

  async stop(signal: NodeJS.Signals = "SIGTERM"): Promise<void> {
    if (!this.child) {
      return;
    }
    const proc = this.child;
    this.child = undefined;
    if (!proc.killed) {
      proc.kill(signal);
    }
    await delay(100);
  }

  private async checkHealth(url: string): Promise<boolean> {
    try {
      const res = await fetch(url, { cache: "no-store" });
      return res.ok;
    } catch {
      return false;
    }
  }
}
