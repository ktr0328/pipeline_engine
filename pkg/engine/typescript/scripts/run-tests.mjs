import { spawn } from "node:child_process";
import { readdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../");
const testDirs = ["src", "test"].map((dir) => path.join(rootDir, dir));

async function collectTests(dir) {
  const files = [];
  try {
    const entries = await readdir(dir, { withFileTypes: true });
    for (const entry of entries) {
      const fullPath = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        files.push(...(await collectTests(fullPath)));
      } else if (entry.isFile() && entry.name.endsWith(".test.ts")) {
        files.push(fullPath);
      }
    }
  } catch {
    // directory may not exist
  }
  return files;
}

const testFiles = [];
for (const dir of testDirs) {
  testFiles.push(...(await collectTests(dir)));
}

if (testFiles.length === 0) {
  console.error("No test files (*.test.ts) found in src/ or test/");
  process.exitCode = 1;
} else {
  const child = spawn(process.execPath, ["--import", "tsx", "--test", ...testFiles], {
    cwd: rootDir,
    stdio: "inherit"
  });
  child.on("exit", (code) => {
    process.exit(code ?? 0);
  });
  child.on("error", (err) => {
    console.error(err);
    process.exit(1);
  });
}
