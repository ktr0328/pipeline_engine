#!/usr/bin/env node
import process from "node:process";
import { PipelineEngineClient } from "../client.js";
import { createMCPAdapterClient, MCPAdapter } from "./adapter.js";

async function main() {
  const baseUrl = process.env.PIPELINE_ENGINE_ADDR ?? "http://127.0.0.1:8085";
  const client = new PipelineEngineClient({ baseUrl });
  const adapter = new MCPAdapter({ client: createMCPAdapterClient(client) });

  const shutdown = () => {
    process.stdin.destroy();
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);

  try {
    await adapter.run();
  } catch (err) {
    console.error("mcp adapter terminated", err);
    process.exitCode = 1;
  }
}

void main();
