// Runs what the previous runtime wrote at its start on an empty data folder, from its own TypeScript sources: the
// single-instance lock (and how it treats a lock left behind), the config and the pair's secret, the cli-token, the
// agents' hook, status line and Codex hook scripts, their settings and MCP files, the prompt editor, the Codex launcher
// and a workspace's prompt files. It writes what they wrote to ../standalone-golden.json, and the two payloads the Go
// runtime reads from the release (workspace-prompts.json, codex-launcher.js) beside this script; the Go test replays
// the same start and must match byte for byte.
//
// The product's words are not spelled here: OLD_WORDS maps them to neutral ones, longest first.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_DATA_DIR_VAR=<its data folder variable> OLD_MODULE_WORD=<the word its own modules
//   start with> OLD_HEALTH_APP=<the app name its health route answers> OLD_WORDS='[["<old>","<neutral>"],...]' \
//   [OLD_FLEET_OUT=<folder for the release's payloads, product words kept>] \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import { syncBuiltinESMExports } from 'node:module';
import os from 'node:os';
import path from 'node:path';

for (const k of ['OLD_RUNTIME_SRC', 'OLD_DATA_DIR_VAR', 'OLD_MODULE_WORD', 'OLD_HEALTH_APP', 'OLD_WORDS']) if (!process.env[k]) throw new Error(`${k} is required`);
const WORDS = JSON.parse(process.env.OLD_WORDS);
const here = path.dirname(new URL(import.meta.url).pathname);

const FIXED = Date.parse('2026-10-19T09:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};
let draws = 0;
crypto.randomBytes = (n) => { draws += 1; const b = Buffer.alloc(n); b.writeUInt32BE(draws, n - 4); return b; };
syncBuiltinESMExports();

const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'standalone-golden-')));
const home = path.join(root, 'home');
const data = path.join(root, 'data');
fs.mkdirSync(home, { recursive: true });
process.env.HOME = home;
process.env[process.env.OLD_DATA_DIR_VAR] = data;
process.umask(0o077);

const neutral = (s) => WORDS.reduce((acc, [from, to]) => acc.split(from).join(to), s.split(data).join('$DATA').split(home).join('$HOME'));
const out = { lock: {}, files: {} };
const files = () => {
  const list = {};
  const walk = (dir) => {
    for (const name of fs.readdirSync(dir).sort()) {
      const p = path.join(dir, name);
      const st = fs.lstatSync(p);
      if (st.isDirectory()) walk(p);
      else list[neutral(path.relative(data, p))] = { content: neutral(fs.readFileSync(p, 'utf8')), mode: (st.mode & 0o777).toString(8) };
    }
  };
  walk(data);
  return list;
};

// --- the lock -----------------------------------------------------------------------------------------------------------
const { acquireLock, releaseLock } = await import('@/lib/lock');
const realExit = process.exit;
const lockName = () => fs.readdirSync(data).find((f) => f.endsWith('.lock'));
const lockCase = async (name, before) => {
  const file = path.join(data, lockName() ?? 'x.lock');
  if (before !== undefined) fs.writeFileSync(file, before);
  let exited = null;
  process.exit = (code) => { exited = code; throw new Error('exit'); };
  try { await acquireLock(8022); } catch (err) { if (err.message !== 'exit') throw err; }
  process.exit = realExit;
  const now = lockName();
  const content = now ? fs.readFileSync(path.join(data, now), 'utf8') : null;
  out.lock[name] = { exited, file: now ? neutral(now) : null,
    content: content === before ? '(kept)' : content?.split(`"pid": ${process.pid},`).join('"pid": $PID,') ?? null };
};
fs.mkdirSync(data, { recursive: true });
await lockCase('fresh');
out.lock.name = neutral(lockName());
await lockCase('garbage', 'not json');
await lockCase('dead holder', JSON.stringify({ pid: 2 ** 22 + 12345, port: 1, startedAt: 1 }));
const serve = (app) => new Promise((resolve) => {
  const srv = http.createServer((req, res) => { res.end(JSON.stringify({ app })); });
  srv.listen(0, '127.0.0.1', () => resolve(srv));
});
const app = process.env.OLD_HEALTH_APP;
for (const [name, answer] of [['live runtime', app], ['live other process', 'other']]) {
  const srv = await serve(answer);
  await lockCase(name, JSON.stringify({ pid: process.ppid, port: srv.address().port, startedAt: 1 }));
  srv.close();
}
fs.writeFileSync(path.join(data, lockName()), JSON.stringify({ pid: process.pid, port: 1, startedAt: 1 }));
releaseLock();
out.lock.released = lockName() === undefined;

// --- the config, the token, the agents' files -------------------------------------------------------------------------
const config = await import('@/lib/config-store');
await config.initConfigStore();
out.files.configCreated = neutral(fs.readFileSync(path.join(data, 'config.json'), 'utf8'));
await config.updateConfig({ authSecret: config.generateSecret() });
const { getCliToken } = await import('@/lib/cli-token');
getCliToken();
const { ensureHookSettings } = await import('@/lib/hook-settings');
out.hookResult = await ensureHookSettings(8022);
const own = (name) => `@/lib/${process.env.OLD_MODULE_WORD}-${name}`;
const editor = await import(own('prompt-editor'));
await editor.ensurePromptEditorScript();
out.editorEnv = neutral(editor.promptEditorEnvPrefix());

// --- a workspace's prompts, and the release's templates ---------------------------------------------------------------
const SENTINEL = 'ws-golden';
const { writeClaudePromptFile } = await import('@/lib/claude-prompt');
const { writeCodexPromptFile } = await import('@/lib/providers/codex/prompt');
await writeClaudePromptFile({ id: SENTINEL, name: 'Golden', directories: [] });
await writeCodexPromptFile({ id: SENTINEL, name: 'Golden', directories: [] });
const prompt = (name) => fs.readFileSync(path.join(data, 'workspaces', SENTINEL, name), 'utf8');
const templates = { claude: prompt('claude-prompt.md').split(SENTINEL).join('{{workspaceId}}'), codex: prompt('codex-prompt.md').split(SENTINEL).join('{{workspaceId}}') };

// The Codex launcher's text is a constant of a module that loads half the runtime: read from its source.
const { PREVIEW_MCP_TOKEN_ENV } = await import(own('preview/devtools/mcp-config'));
const src = fs.readFileSync(path.join(process.env.OLD_RUNTIME_SRC, 'src/lib/providers/codex/index.ts'), 'utf8');
const start = src.indexOf('export const CODEX_LAUNCHER_SCRIPT_CONTENT = `') + 'export const CODEX_LAUNCHER_SCRIPT_CONTENT = '.length;
const launcher = new Function('PREVIEW_MCP_TOKEN_ENV', `return ${src.slice(start, src.indexOf('`;\n', start) + 1)};`)(PREVIEW_MCP_TOKEN_ENV);

out.files.all = files();
out.lockFileName = out.lock.name;
fs.writeFileSync(path.join(here, 'workspace-prompts.json'), neutral(JSON.stringify(templates, null, 2)) + '\n');
fs.writeFileSync(path.join(here, 'codex-launcher.js'), neutral(launcher));
if (process.env.OLD_FLEET_OUT) {
  fs.mkdirSync(process.env.OLD_FLEET_OUT, { recursive: true });
  fs.writeFileSync(path.join(process.env.OLD_FLEET_OUT, 'workspace-prompts.json'), JSON.stringify(templates, null, 2) + '\n');
  fs.writeFileSync(path.join(process.env.OLD_FLEET_OUT, 'codex-launcher.js'), launcher);
}
fs.writeFileSync(path.join(here, '..', 'standalone-golden.json'), JSON.stringify(out, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log('lock cases', Object.keys(out.lock).length, 'files', Object.keys(out.files.all).length);
