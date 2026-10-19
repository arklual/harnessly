#!/usr/bin/env node
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

// TCK-3027: the data directory is ~/.acme-ai, a link to ~/.oldmux on machines that had it.
const baseDir = fs.existsSync(path.join(os.homedir(), '.acme-ai'))
  ? path.join(os.homedir(), '.acme-ai')
  : path.join(os.homedir(), '.oldmux');

const readTrim = (filePath) => {
  try {
    return fs.readFileSync(filePath, 'utf8').trim();
  } catch {
    return '';
  }
};

const parseArgs = (argv) => {
  const result = { workspaceId: null, resumeSessionId: null, model: null, effort: null, prompt: null };
  for (let i = 0; i < argv.length; i += 1) {
    const item = argv[i];
    const next = argv[i + 1];
    if (item === '--workspace-id' && next) {
      result.workspaceId = next;
      i += 1;
    } else if (item.startsWith('--workspace-id=')) {
      result.workspaceId = item.slice('--workspace-id='.length);
    } else if (item === '--resume-session-id' && next) {
      result.resumeSessionId = next;
      i += 1;
    } else if (item.startsWith('--resume-session-id=')) {
      result.resumeSessionId = item.slice('--resume-session-id='.length);
    } else if (item === '--model' && next) {
      result.model = next;
      i += 1;
    } else if (item === '--effort' && next) {
      result.effort = next;
      i += 1;
    } else if (item === '--prompt' && next) {
      result.prompt = next;
      i += 1;
    }
  }
  return result;
};

const fetchArgs = async (payload) => {
  if (typeof fetch !== 'function') {
    throw new Error('Node.js 20+ is required to launch Codex from oldmux');
  }
  const port = readTrim(path.join(baseDir, 'port'));
  if (!port) throw new Error('acme-ai port file is missing');
  const token = readTrim(path.join(baseDir, 'cli-token'));
  const headers = { 'content-type': 'application/json' };
  if (token) headers['x-acme-token'] = headers['x-omux-token'] = token;
  const res = await fetch(`http://127.0.0.1:${port}/api/codex/launch-args`, {
    method: 'POST',
    headers,
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    const message = await res.text().catch(() => '');
    throw new Error(`failed to get Codex launch args: HTTP ${res.status} ${message}`.trim());
  }
  const data = await res.json();
  if (!Array.isArray(data.args) || data.args.some((arg) => typeof arg !== 'string')) {
    throw new Error('invalid Codex launch args response');
  }
  return data.args;
};

const main = async () => {
  const { prompt, ...payload } = parseArgs(process.argv.slice(2));
  const args = await fetchArgs(payload);
  if (prompt) args.push(prompt);
  const token = readTrim(path.join(baseDir, 'cli-token'));
  const env = token ? { ...process.env, ACME_PREVIEW_TOKEN: token } : process.env;
  const child = spawn('codex', args, { stdio: 'inherit', env });
  child.on('exit', (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
      setTimeout(() => process.exit(1), 100);
      return;
    }
    process.exit(code ?? 1);
  });
  child.on('error', (err) => {
    console.error(err.message || String(err));
    process.exit(1);
  });
};

main().catch((err) => {
  console.error(err && err.message ? err.message : String(err));
  process.exit(1);
});
