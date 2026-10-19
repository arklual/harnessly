// Plays dictation scenarios against the previous runtime's own voice socket handler, with a recorded audio file, real
// ffmpeg (or a failing stand-in) and a fake recognition service on a Unix socket, and writes what the page got in each
// to ../golden.json. The Go test plays the same scenarios against its handler and the same fake service.
//
// The fake service answers deterministically: "ready" on start; a partial "pcm <n> <lang>" each time the PCM it got
// passes another 16000 bytes; on stop a final "pcm <total> <lang> <sha256 of the PCM>". Its other modes fail in the
// ways the real one can, or say nothing at all ("silent").
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_NODE_MODULES=<its node_modules, for ws> OLD_VOICE_SOCKET_VAR=<its socket
//   variable> OLD_VOICE_FFMPEG_VAR=<its ffmpeg variable> node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
import crypto from 'node:crypto';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';

const { OLD_VOICE_SOCKET_VAR: SOCKET_VAR, OLD_VOICE_FFMPEG_VAR: FFMPEG_VAR } = process.env;
if (!SOCKET_VAR || !FFMPEG_VAR || !process.env.OLD_RUNTIME_NODE_MODULES) throw new Error('OLD_VOICE_SOCKET_VAR, OLD_VOICE_FFMPEG_VAR and OLD_RUNTIME_NODE_MODULES are required');
const here = path.dirname(new URL(import.meta.url).pathname);
const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'voice-golden-')));
const socketPath = path.join(root, 'asr.sock');
process.env[SOCKET_VAR] = socketPath;

const { WebSocketServer, WebSocket } = await import('ws');
const { handleVoiceConnection } = await import('@/lib/voice-server');

// --- the fake recognition service ------------------------------------------------------------------------------------
let mode = 'normal';
const serve = (conn) => {
  let buffered = '';
  let lang = '';
  let total = 0;
  let audios = 0;
  const hash = crypto.createHash('sha256');
  const emit = (line) => conn.write(line + '\n');
  conn.setEncoding('utf8');
  conn.on('error', () => {});
  conn.on('data', (chunk) => {
    buffered += chunk;
    let at;
    while ((at = buffered.indexOf('\n')) >= 0) {
      const msg = JSON.parse(buffered.slice(0, at));
      buffered = buffered.slice(at + 1);
      if (msg.cmd === 'start') {
        lang = msg.lang;
        if (mode === 'error-before-ready') emit(JSON.stringify({ type: 'error', message: 'Модель не загружена' }));
        else if (mode !== 'silent') emit(JSON.stringify({ type: 'ready' }));
        if (mode === 'malformed') emit('not json\n\n   \n{"type":"partial","text":"a"}\n[1]\n{"type":"unknown"}');
      } else if (msg.cmd === 'audio') {
        audios += 1;
        if (audios === 1 && mode === 'error-after-ready') { emit(JSON.stringify({ type: 'error', message: 42 })); continue; }
        if (audios === 1 && mode === 'close-after-ready') { conn.destroy(); return; }
        if (audios === 1 && mode === 'no-text') emit(JSON.stringify({ type: 'partial' }));
        const pcm = Buffer.from(msg.pcm, 'base64');
        hash.update(pcm);
        const before = Math.floor(total / 16000);
        total += pcm.length;
        for (let k = before + 1; k <= Math.floor(total / 16000); k += 1) emit(JSON.stringify({ type: 'partial', text: `pcm ${k * 16000} ${lang}` }));
      } else if (msg.cmd === 'stop') {
        emit(JSON.stringify(mode === 'no-text' ? { type: 'final' } : { type: 'final', text: `pcm ${total} ${lang} ${hash.digest('hex')}` }));
        conn.end();
      }
    }
  });
};
let service = null;
const listen = () => new Promise((resolve) => { service = net.createServer(serve); service.listen(socketPath, resolve); });
const unlisten = () => new Promise((resolve) => { service.close(() => resolve()); });
await listen();

// --- the handler ------------------------------------------------------------------------------------------------------
const wss = new WebSocketServer({ noServer: true, maxPayload: 256 * 1024 });
wss.on('connection', handleVoiceConnection);
const server = http.createServer();
server.on('upgrade', (req, socket, head) => wss.handleUpgrade(req, socket, head, (ws) => wss.emit('connection', ws, req)));
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const url = `ws://127.0.0.1:${server.address().port}/api/voice`;

const ffmpegReal = execFileSync('sh', ['-c', 'command -v ffmpeg'], { encoding: 'utf8' }).trim();
const ffmpegVersion = execFileSync(ffmpegReal, ['-version'], { encoding: 'utf8' }).split('\n')[0];
const FAILING = '#!/bin/sh\necho "first line" >&2\necho "  pipe:0: Invalid data found when processing input  " >&2\nexit 1\n';
const failing = path.join(root, 'failing-ffmpeg');
fs.writeFileSync(failing, FAILING, { mode: 0o755 });
const audio = fs.readFileSync(path.join(here, '..', 'tone.webm'));
const chunks = [];
for (let i = 0; i < audio.length; i += 1000) chunks.push(audio.subarray(i, i + 1000));

// A scenario: the service's mode, which ffmpeg ("real", "failing", "missing", "relative"), and the page's steps:
// ["text", s] | ["audio"] (the whole file in 1000-byte chunks) | ["binary", n bytes] | ["wait", "ready" | "end" | ms] | ["close"].
const scenarios = [
  ['normal ru', 'normal', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['audio'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['lang en', 'normal', 'real', [['text', '{"type":"start","lang":"en"}'], ['wait', 'ready'], ['audio'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['lang auto', 'normal', 'real', [['text', '{"type":"start","lang":"auto"}'], ['wait', 'ready'], ['audio'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['lang unknown', 'normal', 'real', [['text', '{"type":"start","lang":"xx"}'], ['wait', 'ready'], ['audio'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['lang number', 'normal', 'real', [['text', '{"type":"start","lang":5}'], ['wait', 'ready'], ['audio'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['lang none', 'normal', 'real', [['text', '{"type":"start"}'], ['wait', 'ready'], ['audio'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['noise before start', 'normal', 'real', [['binary', 500], ['text', '{"type":"stop"}'], ['text', 'not json'], ['text', '[1]'], ['text', '{"type":"other"}'],
    ['wait', 200], ['text', '{"type":"start","lang":"en"}'], ['wait', 'ready'], ['text', '{"type":"start","lang":"ru"}'], ['audio'], ['text', '{"type":"stop"}'], ['text', '{"type":"stop"}'], ['wait', 'end'],
    ['audio'], ['text', '{"type":"stop"}'], ['wait', 300]]],
  ['abort', 'normal', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['audio'], ['text', '{"type":"abort"}'], ['wait', 500], ['text', '{"type":"stop"}'], ['wait', 300]]],
  ['close mid-way', 'normal', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['audio'], ['close']]],
  ['stop without audio', 'normal', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['garbage then stop', 'normal', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['binary', 3000], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['ffmpeg fails', 'silent', 'failing', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'end']]],
  ['no ffmpeg', 'normal', 'missing', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'end'], ['text', '{"type":"start","lang":"ru"}'], ['wait', 300]]],
  ['relative ffmpeg', 'normal', 'relative', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'end']]],
  ['no service', 'down', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'end']]],
  ['error before ready', 'error-before-ready', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'end']]],
  ['error after ready', 'error-after-ready', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['audio'], ['wait', 'end']]],
  ['service closes', 'close-after-ready', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['audio'], ['wait', 'end']]],
  ['no text', 'no-text', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['audio'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['malformed lines', 'malformed', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['wait', 300], ['audio'], ['text', '{"type":"stop"}'], ['wait', 'end']]],
  ['message too big', 'normal', 'real', [['text', '{"type":"start","lang":"ru"}'], ['wait', 'ready'], ['binary', 256 * 1024 + 1], ['wait', 'closed']]],
];

const play = (steps) => new Promise((resolve) => {
  const ws = new WebSocket(url);
  const got = [];
  let closeCode = null;
  let waiter = null;
  const check = () => { if (waiter && waiter.test()) { const w = waiter; waiter = null; clearTimeout(w.timer); w.done(); } };
  ws.on('message', (data, isBinary) => { got.push(isBinary ? '<binary>' : data.toString('utf8')); check(); });
  ws.on('close', (code) => { closeCode = code; check(); });
  const waitFor = (test, ms) => new Promise((done) => { waiter = { test, done, timer: setTimeout(() => { waiter = null; done(); }, ms) }; check(); });
  ws.on('open', async () => {
    for (const [kind, arg] of steps) {
      if (kind === 'text') ws.send(arg);
      else if (kind === 'binary') ws.send(Buffer.alloc(arg, 7));
      else if (kind === 'audio') for (const c of chunks) ws.send(c);
      else if (kind === 'close') { ws.close(); await new Promise((r) => setTimeout(r, 300)); }
      else if (arg === 'ready') await waitFor(() => got.some((m) => m.includes('"ready"') || m.includes('"error"')), 5000);
      else if (arg === 'end') await waitFor(() => got.some((m) => m.includes('"final"') || m.includes('"error"')), 8000);
      else if (arg === 'closed') await waitFor(() => closeCode !== null, 5000);
      else await new Promise((r) => setTimeout(r, arg));
    }
    if (ws.readyState === WebSocket.OPEN) ws.close();
    setTimeout(() => resolve({ messages: got, closeCode: closeCode === 1009 ? 1009 : null }), 100);
  });
});

const out = [];
for (const [name, m, ffmpeg, steps] of scenarios) {
  mode = m;
  if (m === 'down') await unlisten();
  process.env[FFMPEG_VAR] = { real: ffmpegReal, failing, missing: path.join(root, 'none'), relative: 'ffmpeg' }[ffmpeg];
  const res = await play(steps);
  if (m === 'down') await listen();
  out.push({ name, mode: m, ffmpeg, steps, ...res });
  console.log(name, JSON.stringify(res).slice(0, 300));
}
server.close();
service.close();
fs.writeFileSync(path.join(here, '..', 'golden.json'), JSON.stringify({ ffmpegVersion, failingFfmpeg: FAILING, scenarios: out }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
process.exit(0);
