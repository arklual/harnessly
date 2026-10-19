// The lines the previous runtime types to start and resume Claude Code and Codex, from its own builders, written to
// ../launch-golden.json for the Go test. The deployment names that must not be spelled here come in from the caller:
//
//   OLD_RUNTIME_SRC=<checkout> OLD_LAUNCHER_VAR=<its managed launcher variable> OLD_EDITOR_VAR=<its human editor
//   variable> OLD_EDITOR_NAME=<its prompt editor script name> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

for (const k of ['OLD_LAUNCHER_VAR', 'OLD_EDITOR_VAR', 'OLD_EDITOR_NAME']) if (!process.env[k]) throw new Error(`${k} is required`);
const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'launch-golden-')));
process.env.HOME = path.join(root, 'home');
fs.mkdirSync(process.env.HOME);
globalThis.__golden = { tmux: { sessions: new Set() }, pasted: [] };

const { dataDir } = await import('@/lib/data-dir');
const { getProvider } = await import('@/lib/providers');
const claudeProvider = getProvider('claude');
const codexProvider = getProvider('codex');
const buildResumeCommand = (id, ws, launch) => claudeProvider.buildResumeCommand(id, { workspaceId: ws, launch });
const DATA = dataDir();
const config = (skip) => fs.writeFileSync(path.join(DATA, 'config.json'), JSON.stringify({ dangerouslySkipPermissions: skip }));
const SID = '425055ec-bbc8-4e3c-87d0-36fa74c1cccb';

const cases = [
  ['claude resume', false, '', () => buildResumeCommand(SID, 'ws-a', null), { provider: 'claude', resume: SID, workspaceId: 'ws-a' }],
  ['claude resume with launch and skip', true, '', () => buildResumeCommand(SID, 'ws-a', { model: 'opus', effort: 'high' }),
    { provider: 'claude', resume: SID, workspaceId: 'ws-a', model: 'opus', effort: 'high' }],
  ['claude launch with launcher', false, '/opt/agents/claude launcher', () => claudeProvider.buildLaunchCommand({ workspaceId: 'ws-b', launch: { model: 'sonnet', effort: null } }),
    { provider: 'claude', workspaceId: 'ws-b', model: 'sonnet', launcher: '/opt/agents/claude launcher' }],
  ['claude launch no workspace', false, '', () => claudeProvider.buildLaunchCommand({}), { provider: 'claude' }],
  ['codex launch', false, '', () => codexProvider.buildLaunchCommand({ workspaceId: 'ws-a' }), { provider: 'codex', workspaceId: 'ws-a' }],
  ['codex resume with launch', true, '', () => codexProvider.buildResumeCommand(SID, { workspaceId: 'ws-a', launch: { model: 'gpt-5.4', effort: 'xhigh' } }),
    { provider: 'codex', resume: SID, workspaceId: 'ws-a', model: 'gpt-5.4', effort: 'xhigh' }],
  ['claude resume bad id', false, '', () => buildResumeCommand('nope', 'ws-a', null), { provider: 'claude', resume: 'nope', workspaceId: 'ws-a' }],
];

const scrub = (s) => s.split(DATA).join('$DATA').split(process.env.OLD_EDITOR_VAR).join('$EDITOR_VAR').split(process.env.OLD_EDITOR_NAME).join('$EDITOR_NAME');
const out = [];
for (const [name, skip, launcher, build, input] of cases) {
  config(skip);
  if (launcher) process.env[process.env.OLD_LAUNCHER_VAR] = launcher;
  else delete process.env[process.env.OLD_LAUNCHER_VAR];
  let line = null;
  let error = null;
  try {
    line = scrub(await build());
  } catch (err) {
    error = err.message;
  }
  out.push({ name, skip, input, line, error });
}
const target = path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'launch-golden.json');
fs.writeFileSync(target, JSON.stringify(out, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} lines → ${target}`);
