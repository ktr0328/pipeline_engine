import { access, chmod, copyFile, mkdir, readFile, writeFile } from "node:fs/promises";
import { constants } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const binDir = path.resolve(__dirname, "../bin");
const binaryName = process.platform === "win32" ? "pipeline-engine.exe" : "pipeline-engine";
const binaryPath = path.join(binDir, binaryName);
const packageJSON = JSON.parse(
  await readFile(path.resolve(__dirname, "../package.json"), "utf8")
);
const packageVersion = packageJSON.version ?? "latest";
const DEFAULT_TEMPLATE = `https://github.com/ktr0328/pipeline_engine/releases/download/v{{version}}/pipeline-engine-{{platform}}-{{arch}}{{ext}}`;

function log(message, quiet) {
  if (!quiet) {
    console.log(`[pipeline-engine/engine] ${message}`);
  }
}

function warn(message) {
  console.warn(`[pipeline-engine/engine] ${message}`);
}

function platformSlug() {
  switch (process.platform) {
    case "darwin":
      return "darwin";
    case "win32":
      return "windows";
    case "linux":
      return "linux";
    default:
      return process.platform;
  }
}

function archSlug() {
  switch (process.arch) {
    case "arm64":
      return "arm64";
    case "x64":
      return "x64";
    default:
      return process.arch;
  }
}

function fileExtension() {
  return process.platform === "win32" ? ".exe" : "";
}

function buildDownloadUrl() {
  const direct = process.env.PIPELINE_ENGINE_ENGINE_DOWNLOAD_URL;
  if (direct) {
    return direct;
  }
  const template =
    process.env.PIPELINE_ENGINE_ENGINE_DOWNLOAD_URL_TEMPLATE ?? DEFAULT_TEMPLATE;
  const version = process.env.PIPELINE_ENGINE_ENGINE_VERSION ?? packageVersion ?? "latest";
  return template
    .replaceAll("{{platform}}", platformSlug())
    .replaceAll("{{arch}}", archSlug())
    .replaceAll("{{version}}", version)
    .replaceAll("{{ext}}", fileExtension());
}

async function downloadBinary(url) {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`failed to download ${url}: ${res.status} ${res.statusText}`);
  }
  const buffer = Buffer.from(await res.arrayBuffer());
  await mkdir(binDir, { recursive: true });
  await writeFile(binaryPath, buffer);
  await chmod(binaryPath, 0o755);
}

export async function ensureBinary({ strict = false, quiet = false } = {}) {
  try {
    await access(binaryPath, constants.X_OK);
    log(`binary already available at ${binaryPath}`, quiet);
    return binaryPath;
  } catch {
    // continue
  }

  const sourcePath = process.env.PIPELINE_ENGINE_ENGINE_SOURCE;
  if (sourcePath) {
    await mkdir(binDir, { recursive: true });
    await copyFile(sourcePath, binaryPath);
    await chmod(binaryPath, 0o755);
    log(`copied binary from ${sourcePath}`, quiet);
    return binaryPath;
  }

  const downloadUrl = buildDownloadUrl();
  if (!downloadUrl) {
    const message = "no binary source configured (set PIPELINE_ENGINE_ENGINE_SOURCE or PIPELINE_ENGINE_ENGINE_DOWNLOAD_URL[_TEMPLATE])";
    if (strict) {
      throw new Error(message);
    }
    warn(`${message}; skipping automatic install`);
    return binaryPath;
  }

  await downloadBinary(downloadUrl);
  log(`downloaded binary from ${downloadUrl}`, quiet);
  return binaryPath;
}

function invokedDirectly() {
  try {
    return pathToFileURL(process.argv[1]).href === import.meta.url;
  } catch {
    return false;
  }
}

if (invokedDirectly()) {
  ensureBinary({ strict: true }).catch((err) => {
    console.error(err);
    process.exitCode = 1;
  });
}
