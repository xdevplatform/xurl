#!/usr/bin/env node

const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

const binary = path.join(__dirname, "binary", process.platform === "win32" ? "xurl.exe" : "xurl");

if (!fs.existsSync(binary)) {
  console.error("xurl binary not found. Try reinstalling: npm install -g @xdevplatform/xurl");
  process.exit(1);
}

// Run the binary as a child we can signal. A supervisor that stops this wrapper
// (an MCP host restarting `xurl mcp`, a process manager, Ctrl+C) must stop the
// binary too, or it keeps running detached with the host's stdio pipes open.
const child = spawn(binary, process.argv.slice(2), { stdio: "inherit" });

const forwarded = ["SIGINT", "SIGTERM", "SIGHUP", "SIGQUIT"];
const relay = (signal) => {
  if (child.exitCode === null && child.signalCode === null) child.kill(signal);
};
for (const signal of forwarded) {
  try {
    process.on(signal, relay);
  } catch (e) {
    // Not every signal can be listened for on every platform.
  }
}

child.on("error", (e) => {
  console.error(`xurl: could not start ${binary}: ${e.message}`);
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    // End the same way the binary did, so callers see the same exit status.
    for (const s of forwarded) process.removeListener(s, relay);
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code === null ? 1 : code);
});
