import { ensureBinary } from "./ensure-binary.mjs";

ensureBinary({ strict: false, quiet: process.env.npm_config_loglevel === "silent" })
  .catch((err) => {
    console.warn(`[pipeline-engine/engine] postinstall skipped: ${err.message}`);
  });
