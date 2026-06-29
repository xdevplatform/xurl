#!/usr/bin/env node

const { execSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");
const http = require("http");

const VERSION = require("./package.json").version;
const REPO = "xdevplatform/xurl";
const BIN_DIR = path.join(__dirname, "binary");

function getPlatform() {
  const platform = os.platform();
  switch (platform) {
    case "darwin": return "Darwin";
    case "linux": return "Linux";
    case "win32": return "Windows";
    default: throw new Error(`Unsupported platform: ${platform}`);
  }
}

function getArch() {
  const arch = os.arch();
  switch (arch) {
    case "x64": return "x86_64";
    case "arm64": return "arm64";
    case "ia32": return "i386";
    default: throw new Error(`Unsupported architecture: ${arch}`);
  }
}

function follow(url) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith("https") ? https : http;
    client.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return follow(res.headers.location).then(resolve, reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
      }
      const chunks = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => resolve(Buffer.concat(chunks)));
      res.on("error", reject);
    }).on("error", reject);
  });
}

async function install() {
  const plat = getPlatform();
  const arch = getArch();
  const ext = plat === "Windows" ? "zip" : "tar.gz";
  const archive = `xurl_${plat}_${arch}.${ext}`;
  const url = `https://github.com/${REPO}/releases/download/v${VERSION}/${archive}`;

  console.log(`Downloading xurl v${VERSION} for ${plat}/${arch}...`);

  const data = await follow(url);

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "xurl-"));
  const archivePath = path.join(tmpDir, archive);
  fs.writeFileSync(archivePath, data);

  fs.mkdirSync(BIN_DIR, { recursive: true });

  if (ext === "tar.gz") {
    execSync(`tar xzf "${archivePath}" -C "${tmpDir}"`);
  } else {
    // Windows releases are .zip. Extract with PowerShell's Expand-Archive,
    // which is built into Windows, instead of the Unix `unzip` command (which
    // isn't present on Windows and broke `npm install -g` there).
    execSync(
      `powershell -NoProfile -NonInteractive -Command "Expand-Archive -LiteralPath '${archivePath}' -DestinationPath '${tmpDir}' -Force"`
    );
  }

  const binaryName = plat === "Windows" ? "xurl.exe" : "xurl";
  const src = path.join(tmpDir, binaryName);
  const dest = path.join(BIN_DIR, binaryName);

  fs.copyFileSync(src, dest);
  fs.chmodSync(dest, 0o755);
  fs.rmSync(tmpDir, { recursive: true, force: true });

  console.log(`xurl v${VERSION} installed to ${dest}`);
}

install().catch((err) => {
  console.error(`Failed to install xurl: ${err.message}`);
  process.exit(1);
});
