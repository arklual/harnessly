// node --test backend/
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { after, before, test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { TOOLS, callTool, handle } from './server.mjs';
import { Store, today } from './store.mjs';

const here = dirname(fileURLToPath(import.meta.url));
let dir;
const fixed = () => new Date(2026, 10, 3, 9, 30);
before(() => { dir = mkdtempSync(join(tmpdir(), 'standup-test-')); });
after(() => rmSync(dir, { recursive: true, force: true }));
const fresh = () => new Store(mkdtempSync(join(dir, 's-')), fixed);

test('today is the local date', () => assert.equal(today(fixed()), '2026-11-03'));

test('an empty day lists no entries', () => {
  assert.deepEqual(fresh().load(), { date: '2026-11-03', entries: [] });
});

test('save_entry creates, then replaces the same person (any case) without a second row', () => {
  const s = fresh();
  callTool(s, 'save_entry', { person: 'Дана', yesterday: 'ревью', today: 'логин', blockers: '' });
  callTool(s, 'save_entry', { person: ' дана ', yesterday: 'ревью', today: 'релиз', blockers: 'нет' });
  const { entries } = s.load();
  assert.equal(entries.length, 1);
  assert.equal(entries[0].today, 'релиз');
  assert.equal(entries[0].updatedBy, 'agent');
});

test('an entry keeps the texts a change does not name', () => {
  const s = fresh();
  s.upsert({ person: 'Дана', yesterday: 'ревью', today: 'логин', blockers: 'нет' });
  const e = s.upsert({ person: 'Дана', today: 'релиз' });
  assert.deepEqual([e.yesterday, e.today, e.blockers], ['ревью', 'релиз', 'нет']);
});

test('no call empties an entry, but a new entry may start with one text', () => {
  const s = fresh();
  s.upsert({ person: 'Дана', today: 'логин' });
  assert.throws(() => s.upsert({ person: 'Дана', yesterday: ' ', today: '', blockers: '' }), /empty/);
  assert.equal(s.load().entries[0].today, 'логин');
});

test('an entry the person edited changes only with the updatedAt the agent read', () => {
  const s = fresh();
  const made = s.upsert({ person: 'Дана', today: 'логин' });
  const doc = s.load();
  doc.entries[0] = { ...doc.entries[0], today: 'правка человека', updatedBy: 'person', updatedAt: '2026-11-03T07:00:00.000Z' };
  s.save(doc);
  assert.throws(() => s.upsert({ person: 'Дана', today: 'версия агента' }), /person edited/);
  assert.throws(() => s.upsert({ person: 'Дана', today: 'версия агента', expectedUpdatedAt: made.updatedAt }), /person edited/);
  assert.equal(s.load().entries[0].today, 'правка человека');
  const e = s.upsert({ person: 'Дана', today: 'версия агента', expectedUpdatedAt: '2026-11-03T07:00:00.000Z' });
  assert.equal(e.today, 'версия агента');
});

test('what the view wrote is read back, with its updatedBy', () => {
  const s = fresh();
  s.upsert({ person: 'Дана', today: 'логин' });
  const doc = s.load();
  doc.entries[0].today = 'правка человека';
  doc.entries[0].updatedBy = 'person';
  writeFileSync(s.file('2026-11-03'), JSON.stringify(doc));
  assert.equal(callTool(s, 'list_entries').entries[0].updatedBy, 'person');
});

test('a person is required and a date must be a date', () => {
  const s = fresh();
  assert.throws(() => s.upsert({ person: '  ' }), /person/);
  assert.throws(() => s.load('../../etc/passwd'), /YYYY-MM-DD/);
  assert.throws(() => callTool(s, 'save_entry', { person: 'x', date: '../x' }), /YYYY-MM-DD/);
});

test('the agent has no tool that deletes', () => {
  assert.deepEqual(TOOLS.map((t) => t.name).sort(), ['list_entries', 'save_entry', 'show_standup']);
  assert.throws(() => callTool(fresh(), 'delete_entry', {}), /unknown tool/);
  const res = handle(fresh(), { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'delete_entry', arguments: {} } });
  assert.equal(res.error.code, -32602);
});

test('show_standup names the card view, and its text is one line the runtime can read whole', () => {
  const s = fresh();
  s.upsert({ person: 'Дана', today: 'логин' });
  const res = handle(s, { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'show_standup', arguments: {} } });
  assert.equal(res.result._meta.ui.resourceUri, 'ui://harnessly.standup/card');
  assert.deepEqual(res.result.structuredContent, { date: '2026-11-03' });
  const text = res.result.content[0].text;
  // The feed reads a tool result as the first line of its text, cut at 100 characters (internal/transcript).
  assert.ok(text.length <= 100 && !text.includes('\n'), `${text.length} characters`);
  assert.deepEqual(JSON.parse(text), { _meta: { ui: { resourceUri: 'ui://harnessly.standup/card' } }, data: { date: '2026-11-03' } });
  assert.equal(handle(s, { jsonrpc: '2.0', id: 3, method: 'tools/call', params: { name: 'list_entries', arguments: {} } }).result._meta, undefined);
});

test('the manifest card view and uri agree, and the manifest rules are the ones the README claims', () => {
  const app = JSON.parse(readFileSync(join(here, '..', 'harnessly', 'app.json'), 'utf8'));
  const card = app.views.find((v) => v.id === 'card');
  assert.deepEqual(card.targets, ['chat']);
  assert.equal(`ui://${app.id}/${card.id}`, 'ui://harnessly.standup/card');
  const deny = app.agent.tools.deny;
  for (const tool of ['Edit', 'Write', 'MultiEdit', 'NotebookEdit']) assert.ok(deny.some((r) => r.startsWith(`${tool}(`)), tool);
  assert.equal(app.agent.tools.allow, undefined);
});

test('over stdio: initialize, tools/list, tools/call, and the file lands in STANDUP_DIR', async () => {
  const data = join(dir, 'stdio');
  const child = spawn(process.execPath, [join(here, 'server.mjs')], { env: { ...process.env, STANDUP_DIR: data }, stdio: ['pipe', 'pipe', 'inherit'] });
  const lines = [];
  let buf = '';
  child.stdout.on('data', (c) => { buf += c; let i; while ((i = buf.indexOf('\n')) >= 0) { lines.push(JSON.parse(buf.slice(0, i))); buf = buf.slice(i + 1); } });
  const send = (m) => child.stdin.write(`${JSON.stringify(m)}\n`);
  const reply = async (id) => { for (let n = 0; n < 100; n++) { const r = lines.find((l) => l.id === id); if (r) return r; await new Promise((r2) => setTimeout(r2, 20)); } throw new Error(`no answer ${id}`); };
  send({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 't', version: '0' } } });
  assert.equal((await reply(1)).result.serverInfo.name, 'standup');
  send({ jsonrpc: '2.0', method: 'notifications/initialized' });
  send({ jsonrpc: '2.0', id: 2, method: 'tools/list' });
  assert.equal((await reply(2)).result.tools.length, 3);
  send({ jsonrpc: '2.0', id: 3, method: 'tools/call', params: { name: 'save_entry', arguments: { person: 'Дана', today: 'логин', date: '2026-11-03' } } });
  assert.equal((await reply(3)).result.isError, undefined);
  child.stdin.end();
  assert.equal(JSON.parse(readFileSync(join(data, '2026-11-03.json'), 'utf8')).entries[0].person, 'Дана');
  assert.equal(lines.length, 3, 'a notification gets no answer');
});
