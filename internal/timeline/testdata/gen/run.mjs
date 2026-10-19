// Plays ../scenario.json against the previous runtime's own timeline socket (its TypeScript sources, with the stubs of
// this folder) and writes what every socket received, and what the layout store and the keeper were told, to
// ../golden.json. The Go test plays the same scenario against its server and must get the same.
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> OLD_RUNTIME_MODULES=<node_modules of its release> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs run.mjs
//
// Intervals of ten seconds and more (the agent process polls, the heartbeat) never fire here: their moment relative to
// the scenario's steps is chance. The Go test turns them off too.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { normalize } from './normalize.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const FIXED = Date.parse('2026-10-18T12:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};
const realSetInterval = globalThis.setInterval;
globalThis.setInterval = (fn, ms, ...rest) => (ms >= 10_000 ? realSetInterval(() => {}, 1 << 30) : realSetInterval(fn, ms, ...rest));

process.env.TZ = 'UTC';
const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'timeline-golden-')));
const home = path.join(root, 'home');
fs.mkdirSync(home);
process.env.HOME = home;

globalThis.__world = { panes: {}, procs: {}, busy: new Set(), recover: {}, effects: [] };
const world = globalThis.__world;

const { dataDir } = await import('@/lib/data-dir');
const { handleTimelineConnection, broadcastSessionStats } = await import('@/lib/timeline-server');
const { codexHookEvents } = await import('@/lib/providers/codex/hook-events');
const { WebSocketServer, WebSocket } = await import('ws');

const vars = { home, claude: path.join(home, '.claude'), codex: path.join(home, '.codex'), data: dataDir() };
const expand = (v) => {
  if (typeof v === 'string') return v.replace(/\{(home|claude|codex|data)\}/g, (_, k) => vars[k]);
  if (Array.isArray(v)) return v.map(expand);
  if (v && typeof v === 'object') return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, expand(x)]));
  return v;
};

const wss = new WebSocketServer({ port: 0, perMessageDeflate: { threshold: 1024 } });
wss.on('connection', (ws, req) => { void handleTimelineConnection(ws, req); });
await new Promise((resolve) => wss.once('listening', resolve));
const port = wss.address().port;

const scenario = JSON.parse(fs.readFileSync(path.join(here, '..', 'scenario.json'), 'utf8'));
const fixtures = path.join(here, '..', 'fixtures');
const sockets = {};
const received = {};
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

for (const raw of scenario.steps) {
  const step = expand(raw);
  switch (step.op) {
    case 'mkdir': fs.mkdirSync(step.path, { recursive: true }); break;
    case 'copy': fs.copyFileSync(path.join(fixtures, step.from), step.path); break;
    case 'append': fs.appendFileSync(step.path, fs.readFileSync(path.join(fixtures, step.from))); break;
    case 'appendText': fs.appendFileSync(step.path, step.text); break;
    case 'write': fs.writeFileSync(step.path, JSON.stringify(step.data)); break;
    case 'rm': fs.rmSync(step.path, { force: true }); break;
    case 'pane': world.panes[step.session] = { ...(world.panes[step.session] ?? {}), ...step.pane }; break;
    case 'proc': world.procs[String(step.pid)] = step.proc; break;
    case 'noproc': delete world.procs[String(step.pid)]; break;
    case 'busy': if (step.on) world.busy.add(step.session); else world.busy.delete(step.session); break;
    case 'recover': world.recover[step.session] = step.found; break;
    case 'open': {
      const ws = new WebSocket(`ws://127.0.0.1:${port}/api/timeline?${step.query}`, { perMessageDeflate: true });
      const list = (received[step.socket] = []);
      ws.on('message', (data) => list.push(JSON.parse(data.toString())));
      ws.on('close', (code, reason) => list.push({ close: code, reason: reason.toString() }));
      sockets[step.socket] = ws;
      await new Promise((resolve) => { ws.once('open', resolve); ws.once('close', resolve); });
      break;
    }
    case 'send': sockets[step.socket].send(step.raw ?? JSON.stringify(step.data)); break;
    case 'close': sockets[step.socket].close(); break;
    case 'stats': broadcastSessionStats(step.data); break;
    case 'codexHook': codexHookEvents.emit('session-info', step.session, step.info); break;
    case 'wait': await sleep(step.ms); break;
    default: throw new Error(`unknown step ${step.op}`);
  }
}

for (const ws of Object.values(sockets)) if (ws.readyState === WebSocket.OPEN) ws.close();
await sleep(300);
const out = normalize({ sockets: received, effects: world.effects }, vars);
fs.writeFileSync(path.join(here, '..', 'golden.json'), JSON.stringify(out, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
process.exit(0);
