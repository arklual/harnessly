// Asks the previous runtime's own dispatcher and voice assets handler for the in-tab engine's files, with the model
// hosts answered by a local server, and its allow-list for generated paths; writes what each answered to
// ../assets.json. The Go test serves the same upstream and must answer alike.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_RUNTIME_TOKEN_HEADERS=<current,legacy> OLD_DATA_DIR_VAR=<its data folder variable> \
//     node --no-warnings --experimental-transform-types --import ./register.mjs assets.mjs
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import zlib from 'node:zlib';

const here = path.dirname(new URL(import.meta.url).pathname);
const OLD_TOKENS = (process.env.OLD_RUNTIME_TOKEN_HEADERS ?? '').split(',');
if (OLD_TOKENS.length !== 2 || !process.env.OLD_DATA_DIR_VAR) throw new Error('OLD_RUNTIME_TOKEN_HEADERS and OLD_DATA_DIR_VAR are required');
const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'voice-assets-')));
process.env[process.env.OLD_DATA_DIR_VAR] = root;
const TOKEN = 'd'.repeat(64);
fs.writeFileSync(path.join(root, 'cli-token'), TOKEN);

// --- the allow-list on generated paths --------------------------------------------------------------------------------
const { resolveVoiceAsset } = await import('@/lib/voice-assets');
let seed = 9;
const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
const pick = (a) => a[Math.floor(rnd() * a.length)];
const parts = ['hf', 'ort', 'onnx-community', 'whisper-small', 'whisper-base', 'resolve', 'main', 'dev', 'config.json', 'tokenizer.json', 'vocab.txt',
  'Config.json', 'a_b.json', 'onnx', 'encoder_model.onnx', 'decoder_model_merged_q4.onnx', 'encoder_model_fp16.onnx_data', 'decoder_model_merged.onnx',
  'encoder_model_int8.onnx', '3.7.1', '10.20.30', '1.2', 'v3.0.0', 'ort-wasm-simd-threaded.jsep.wasm', 'ort-wasm-simd-threaded.jsep.mjs', 'ort-wasm.wasm', '', '..', 'x/y'];
const resolved = [];
for (let i = 0; i < 400; i++) {
  let segs = Array.from({ length: 1 + Math.floor(rnd() * 7) }, () => pick(parts));
  if (rnd() < 0.4) segs = ['hf', 'onnx-community', 'whisper-small', 'resolve', 'main', ...segs.slice(0, 2)];
  if (rnd() < 0.15) segs = ['ort', pick(parts), pick(parts)];
  resolved.push([segs, resolveVoiceAsset(segs)]);
}

// --- the route, the model hosts answered here -------------------------------------------------------------------------
const files = { 'config.json': '{"model_type":"whisper"}', 'vocab.txt': 'a\nb\n', 'encoder_model_q4.onnx': 'onnx-bytes-'.repeat(50) };
const upstream = http.createServer((req, res) => {
  const file = req.url.split('/').pop();
  if (req.url.includes('tokenizer.json')) { res.writeHead(404); res.end('no'); return; }
  if (req.url.includes('ort-wasm-simd-threaded.jsep.mjs')) {
    const body = zlib.gzipSync('export const x = 1;\n');
    res.writeHead(200, { 'content-encoding': 'gzip', 'content-length': body.length, 'content-type': 'text/javascript' });
    res.end(body);
    return;
  }
  if (req.url.includes('ort-wasm-simd-threaded.jsep.wasm')) { res.writeHead(302, { location: '/moved/wasm-bytes' }); res.end(); return; }
  if (req.url.endsWith('/moved/wasm-bytes')) { res.writeHead(200, { 'content-length': 10 }); res.end('\0asm012345'); return; }
  if (files[file] !== undefined) { res.writeHead(200, { 'content-length': Buffer.byteLength(files[file]) }); res.end(files[file]); return; }
  res.writeHead(500); res.end();
});
await new Promise((r) => upstream.listen(0, '127.0.0.1', r));
const local = `http://127.0.0.1:${upstream.address().port}`;
const realFetch = globalThis.fetch;
globalThis.fetch = (url, opts) => realFetch(String(url).replace('https://huggingface.co', `${local}/hf-host`).replace('https://cdn.jsdelivr.net', `${local}/cdn-host`), opts);

const cached = path.join(root, 'voice-assets', 'hf', 'onnx-community', 'whisper-small', 'generation_config.json');
fs.mkdirSync(path.dirname(cached), { recursive: true });
fs.writeFileSync(cached, '{"cached":true}');

const { createApiDispatcher } = await import('@/lib/runtime-api/dispatch');
const dispatch = createApiDispatcher([{ route: '/api/voice/assets/[...path]', load: () => import('@/pages/api/voice/assets/[...path]') }]);
const server = http.createServer((req, res) => { dispatch(req, res).then((handled) => { if (!handled) { res.statusCode = 404; res.end('not handled'); } }); });
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

const M = '/api/voice/assets/hf/onnx-community/whisper-small/resolve/main';
const ses = { cookie: 'session-token=good' };
const requests = [
  [`${M}/config.json`, {}],
  [`${M}/config.json`, { 'x-cli-token': TOKEN }],
  [`${M}/config.json`, ses],
  [`${M}/config.json`, ses],
  [`${M}/generation_config.json`, ses],
  [`${M}/vocab.txt`, ses],
  [`${M}/onnx/encoder_model_q4.onnx`, ses],
  [`${M}/tokenizer.json`, ses],
  [`${M}/preprocessor_config.json`, ses],
  [`${M}/model.safetensors`, ses],
  [`${M}/onnx%2Fencoder_model_q4.onnx`, ses],
  ['/api/voice/assets/ort/3.7.1/ort-wasm-simd-threaded.jsep.mjs', ses],
  ['/api/voice/assets/ort/3.7.1/ort-wasm-simd-threaded.jsep.wasm', ses],
  ['/api/voice/assets/ort/3.7/ort-wasm-simd-threaded.jsep.wasm', ses],
  ['/agents/api/voice/assets/hf/onnx-community/whisper-small/resolve/main/vocab.txt/', ses],
  ['/api/voice/assets/x', ses],
];
const toOld = (h) => Object.fromEntries(Object.entries(h).map(([k, v]) => [k === 'x-cli-token' ? OLD_TOKENS[0] : k, v]));
const KEEP = ['content-type', 'content-length', 'cache-control', 'etag'];
const out = [];
for (const [p, headers] of requests) {
  const res = await new Promise((resolve) => {
    http.get(base + p, { headers: toOld(headers) }, (r) => {
      const chunks = [];
      r.on('data', (c) => chunks.push(c));
      r.on('end', () => resolve({ status: r.statusCode, headers: r.headers, body: Buffer.concat(chunks) }));
    });
  });
  await new Promise((r) => setTimeout(r, 50));
  const kept = {};
  for (const k of KEEP) if (res.headers[k] !== undefined) kept[k] = String(res.headers[k]);
  out.push({ path: p, headers, status: res.status, answerHeaders: kept, body: res.body.toString('base64') });
}
const tree = [];
const walk = (d) => { for (const n of fs.readdirSync(d).sort()) { const f = path.join(d, n); if (fs.statSync(f).isDirectory()) walk(f); else tree.push([path.relative(root, f), fs.readFileSync(f).toString('base64')]); } };
walk(path.join(root, 'voice-assets'));
server.close();
upstream.close();
fs.writeFileSync(path.join(here, '..', 'assets.json'), JSON.stringify({ token: TOKEN, resolved, requests: out, cache: tree }, null, 1) + '\n');
fs.rmSync(root, { recursive: true, force: true });
console.log(`${out.length} requests, ${resolved.length} paths → ${path.join(here, '..', 'assets.json')}`);
process.exit(0);
