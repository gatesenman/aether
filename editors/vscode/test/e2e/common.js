const vscode = require("vscode");
const cp = require("child_process");
const fs = require("fs");
const path = require("path");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function waitFor(fn, ms = 30000) {
  const t = Date.now();
  while (Date.now() - t < ms) { const v = await fn(); if (v) return v; await sleep(250); }
  throw new Error("timeout");
}
const SHOTS = process.env.AETHER_E2E_SHOTS;
function shot(name) { // real screen capture of the IDE window on the Xvfb display
  const out = path.join(SHOTS, name + ".png");
  const r = cp.spawnSync("ffmpeg", ["-loglevel", "error", "-y", "-f", "x11grab", "-video_size", process.env.AETHER_E2E_SIZE || "1600x1000", "-i", process.env.DISPLAY, "-frames:v", "1", out]);
  return r.status === 0 && fs.existsSync(out);
}
function runner(outFile) {
  const results = [];
  const step = async (name, f) => {
    try { await f(); results.push({ name, ok: true }); } catch (e) { results.push({ name, ok: false, error: String((e && e.stack) || e) }); }
    fs.writeFileSync(outFile, JSON.stringify(results, null, 1));
  };
  return { results, step };
}
module.exports = { vscode, sleep, waitFor, shot, runner, fs, path, cp };
