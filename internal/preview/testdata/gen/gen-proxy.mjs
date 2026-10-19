// The previous runtime's preview proxy end to end: a dev server, its neighbour and a stand (https) behind it, requests
// with claims of every kind, WebSockets, DevTools conditions, the stand asset cache. Writes ../proxy.json: the fixture
// (with $PORT, $NPORT and $STAND standing for the addresses), every request and what came back, and the DevTools log at
// the end. Ports, cookie tags and the stand's host are written as placeholders, so the Go test fills in its own.
//
//   OLD_RUNTIME_SRC=<checkout> NODE_MODULES=<its node_modules> PRODUCT=… PRODUCT_DOMAIN=… CLI=… \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen-proxy.mjs
import fs from 'node:fs';
import http from 'node:http';
import https from 'node:https';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import zlib from 'node:zlib';
import { execFileSync } from 'node:child_process';
import { createHmac } from 'node:crypto';
import { once } from 'node:events';
import { FIXED, neutral, real } from './common.mjs';
import { fixtureRoutes, encodeBody } from './fixture.mjs';

const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};
const P = process.env.PRODUCT;
const data = fs.mkdtempSync(path.join(os.tmpdir(), 'preview-proxy-'));
process.env.GOLDEN_DATA_DIR = data;
const SECRET = 'golden-proxy-secret-0123456789abcdef';
const lib = (rel) => import(`@/lib/${P}-${rel}`);
const proxy = await lib('preview/proxy');
const claimMod = await lib('preview/claim');
const store = await lib('preview/devtools/store');
const ssrf = await lib('preview/ssrf');

const STAND_HOST = `st${real.standDomain}`;
const STAND = `https://${STAND_HOST}`;
const certDir = fs.mkdtempSync(path.join(os.tmpdir(), 'preview-proxy-tls-'));
execFileSync('openssl', ['req', '-x509', '-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:prime256v1', '-nodes', '-days', '1', '-subj', `/CN=${STAND_HOST}`,
  '-addext', `subjectAltName=DNS:${STAND_HOST}`, '-keyout', path.join(certDir, 'key.pem'), '-out', path.join(certDir, 'cert.pem')], { stdio: 'ignore' });
const tls = { key: fs.readFileSync(path.join(certDir, 'key.pem')), cert: fs.readFileSync(path.join(certDir, 'cert.pem')) };

const table = fixtureRoutes('$PORT', '$NPORT', '$STAND');
const ports = {};
const fill = (s) => s.replaceAll('$PORT', String(ports.main)).replaceAll('$NPORT', String(ports.neighbour)).replaceAll('$STAND', STAND);
const serveFixture = (req, res) => {
  const url = new URL(req.url, 'http://x');
  if (url.pathname === '/echo' || url.pathname === '/remote.js') {
    const chunks = [];
    req.on('data', (c) => chunks.push(c));
    req.on('end', () => {
      const headers = Object.fromEntries(Object.entries(req.headers).filter(([k]) => !['connection', 'keep-alive', 'transfer-encoding'].includes(k)).sort(([a], [b]) => (a < b ? -1 : 1)));
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ method: req.method, url: req.url, headers, body: Buffer.concat(chunks).toString() }));
    });
    return;
  }
  const route = table[url.pathname];
  if (!route) { res.writeHead(404); res.end(); return; }
  const filled = { ...route, body: fill(route.body) };
  const body = encodeBody(filled);
  const lines = [];
  for (const [k, v] of route.headers) lines.push(k, fill(v));
  if (!route.headers.some(([k]) => k.toLowerCase() === 'content-length') && route.status !== 204) lines.push('Content-Length', String(body.length));
  res.writeHead(route.status, lines);
  res.end(req.method === 'HEAD' ? undefined : body);
};
const upgradeFixture = (req, socket, head) => {
  const url = new URL(req.url, 'http://x');
  if (url.pathname === '/ws-reject') { socket.end('HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n'); return; }
  socket.write(['HTTP/1.1 101 Switching Protocols', 'Upgrade: websocket', 'Connection: Upgrade', 'Sec-WebSocket-Accept: abc=', 'Set-Cookie: hmr=1; Path=/; Domain=localhost',
    'Set-Cookie: _oauth2_proxy=s', `X-Seen: ${req.headers.cookie ?? ''}|${req.headers.origin ?? ''}|${req.headers.host}`, '', ''].join('\r\n'));
  if (head?.length) socket.write(Buffer.concat([Buffer.from('echo:'), head]));
  socket.on('data', (d) => socket.write(Buffer.concat([Buffer.from('echo:'), d])));
  socket.on('error', () => {});
};
const listen = async (server) => { server.listen(0, '127.0.0.1'); await once(server, 'listening'); return server.address().port; };
const main = http.createServer(serveFixture); main.on('upgrade', upgradeFixture);
const neighbour = http.createServer(serveFixture);
const stand = https.createServer(tls, serveFixture); stand.on('upgrade', upgradeFixture);
ports.main = await listen(main);
ports.neighbour = await listen(neighbour);
ports.stand = await listen(stand);
const RESERVED = 18044;

// The stand answers on its own port whatever port the proxy asks for: the stand's origin has none.
class StandAgent extends https.Agent {
  createConnection(options, cb) { return super.createConnection({ ...options, port: ports.stand }, cb); }
}
const options = {
  forbiddenPorts: new Set([RESERVED]),
  secret: SECRET,
  httpsAgent: new StandAgent({ ca: tls.cert, keepAlive: false }),
  resolveTarget: async (host) => {
    const map = { 'site.example.org': '127.0.0.1', [STAND_HOST]: '127.0.0.1' };
    if (map[host]) return { address: map[host], family: 4 };
    return ssrf.resolvePublic(host, { localAddresses: new Set() });
  },
  assetCache: { dir: path.join(data, 'preview-cache') },
};
const runtime = http.createServer((req, res) => {
  if (!proxy.isPreviewPath(req.url)) { res.writeHead(404); res.end(); return; }
  void proxy.handlePreviewRequest(req, res, options);
});
runtime.on('upgrade', (req, socket, head) => { void proxy.handlePreviewUpgrade(req, socket, head, options); });
const rport = await listen(runtime);

const AUD = claimMod.PREVIEW_CLAIM_AUDIENCE;
const b64 = (v) => Buffer.from(v).toString('base64url');
const sign = (payload) => {
  const head = `${b64(JSON.stringify({ alg: 'HS256' }))}.${b64(JSON.stringify({ ...payload, aud: AUD, exp: Math.floor(FIXED / 1000) + 3600 }))}`;
  return `${head}.${createHmac('sha256', SECRET).update(head).digest('base64url')}`;
};
const SLOT = 'https://ai-alice-1.example.org';
const URLSLOT = 'https://ai-alice-2.example.org';
const STANDSLOT = 'https://ai-alice-3.example.org';
const PARENT = 'https://app.example.org';
const claims = {
  port: { sub: 'alice', target: { kind: 'port', port: '$PORT' }, slotOrigin: SLOT, portSlots: { $PORT: SLOT, $NPORT: `${SLOT}/__tk/p/$NPORT` }, parentOrigin: PARENT },
  portNoParent: { sub: 'alice', target: { kind: 'port', port: '$PORT' }, slotOrigin: SLOT, portSlots: { $PORT: SLOT } },
  reserved: { sub: 'alice', target: { kind: 'port', port: RESERVED }, slotOrigin: SLOT, portSlots: { [RESERVED]: SLOT } },
  url: { sub: 'alice', target: { kind: 'url', origin: 'http://site.example.org:$PORT' }, slotOrigin: URLSLOT, portSlots: {}, parentOrigin: PARENT },
  internal: { sub: 'alice', target: { kind: 'url', origin: 'http://10.1.2.3' }, slotOrigin: URLSLOT, portSlots: {} },
  loopback: { sub: 'alice', target: { kind: 'url', origin: 'http://127.0.0.1:$PORT' }, slotOrigin: URLSLOT, portSlots: {} },
  missing: { sub: 'alice', target: { kind: 'url', origin: 'http://name.invalid' }, slotOrigin: URLSLOT, portSlots: {} },
  stand: { sub: 'alice', target: { kind: 'url', origin: '$STAND' }, slotOrigin: STANDSLOT, portSlots: {}, parentOrigin: PARENT },
};
const fillClaim = (c) => JSON.parse(fill(JSON.stringify(c)).replace(/"port":"(\d+)"/, '"port":$1'));
const tagOf = (c) => claimMod.targetCookieTag(fillClaim(c).target, SECRET);
const tags = { $TAG: tagOf(claims.port), $UTAG: tagOf(claims.url), $STAG: tagOf(claims.stand) };
const fillReq = (s) => {
  let out = fill(s);
  for (const [k, v] of Object.entries(tags)) out = out.split(k).join(v);
  return out;
};
const unfill = (s) => {
  let out = s;
  for (const [k, v] of Object.entries(tags)) out = out.split(v).join(k);
  return out.split(STAND).join('$STAND').split(`:${ports.main}`).join(':$PORT').split(`/${ports.main}`).join('/$PORT').split(`"${ports.main}"`).join('"$PORT"')
    .split(`/0${ports.neighbour}`).join('/0$NPORT').split(`:${ports.neighbour}`).join(':$NPORT').split(`/${ports.neighbour}`).join('/$NPORT').split(`=${ports.neighbour}`).join('=$NPORT');
};
const PROXY = `/agents/api/${P}/preview/proxy`;
const HEADER = real.claimHeader;
const COMPARED = ['content-type', 'cache-control', 'location', 'set-cookie', 'content-encoding', 'vary', 'etag', 'last-modified', 'expires', 'x-robots-tag', 'x-tk-devtools',
  'x-tk-asset-cache', 'content-security-policy', 'content-security-policy-report-only', 'x-frame-options', 'clear-site-data', 'x-content-type-options', 'content-length'];
const steps = [];
const decode = (coding, buf) => (coding === 'br' ? zlib.brotliDecompressSync(buf) : coding === 'gzip' ? zlib.gunzipSync(buf) : buf);
const request = (name, claim, rel, { method = 'GET', headers = {}, body } = {}) => new Promise((resolve) => {
  const h = { ...headers };
  if (claim) h[HEADER] = claim === 'bad' ? 'not-a-token' : sign(fillClaim(claims[claim]));
  for (const [k, v] of Object.entries(h)) h[k] = Array.isArray(v) ? v.map(fillReq) : fillReq(v);
  const req = http.request({ host: '127.0.0.1', port: rport, method, path: PROXY + fill(rel), headers: h }, (res) => {
    const chunks = [];
    res.on('data', (c) => chunks.push(c));
    res.on('end', () => {
      const raw = Buffer.concat(chunks);
      const coding = res.headers['content-encoding'];
      const kept = {};
      for (const k of COMPARED) if (res.headers[k] !== undefined) kept[k] = Array.isArray(res.headers[k]) ? res.headers[k].map(unfill) : unfill(String(res.headers[k]));
      if (coding === 'br' || coding === 'gzip') delete kept['content-length'];
      let text;
      try { text = decode(['br', 'gzip'].includes(coding) ? coding : null, raw).toString('base64'); } catch { text = raw.toString('base64'); }
      const out = { status: res.statusCode, headers: kept, body: Buffer.from(neutral(unfill(Buffer.from(text, 'base64').toString('latin1'))), 'latin1').toString('base64') };
      steps.push({ kind: 'http', name, claim, path: rel, method, headers, body: body ?? null, out });
      resolve();
    });
  });
  req.on('error', (e) => { steps.push({ kind: 'http', name, claim, path: rel, method, headers, body: body ?? null, out: { error: e.code } }); resolve(); });
  req.end(body === undefined ? undefined : fillReq(body));
});
const upgrade = (name, claim, rel, headers = {}) => new Promise((resolve) => {
  const sock = net.connect(rport, '127.0.0.1');
  const h = { host: 'runtime', connection: 'Upgrade', upgrade: 'websocket', ...headers };
  if (claim) h[HEADER] = sign(fillClaim(claims[claim]));
  sock.write(`GET ${PROXY}${fill(rel)} HTTP/1.1\r\n${Object.entries(h).map(([k, v]) => `${k}: ${fillReq(v)}`).join('\r\n')}\r\n\r\nhello`);
  let buf = Buffer.alloc(0);
  sock.on('data', (d) => {
    buf = Buffer.concat([buf, d]);
    const text = buf.toString('latin1');
    if (text.includes('echo:hello') || (text.includes('\r\n\r\n') && !text.startsWith('HTTP/1.1 101'))) sock.destroy();
  });
  const finish = () => { steps.push({ kind: 'upgrade', name, claim, path: rel, headers, out: unfill(buf.toString('latin1')) }); resolve(); };
  sock.on('close', finish);
  sock.on('error', () => {});
  setTimeout(() => sock.destroy(), 3000);
});
const op = (o) => steps.push({ kind: 'op', ...o });
const settings = (slot, patch) => { store.setSettings(slot, patch); op({ op: 'settings', slot, patch }); };

const doc = { 'sec-fetch-dest': 'document', accept: 'text/html' };
await request('no claim', null, '/page');
await request('bad claim', 'bad', '/page');
await request('reserved port', 'reserved', '/page');
await request('neighbour not named', 'portNoParent', '/__tk/p/$NPORT/remote.js');
await request('internal address', 'internal', '/page', { headers: doc });
await request('loopback address', 'loopback', '/page', { headers: doc });
await request('unknown name', 'missing', '/page', { headers: doc });
await request('page with bridge', 'port', '/page', { headers: { ...doc, cookie: '__t_$TAG_sid=1; other=2; __t_x_y=3', 'x-acme-gateway': '1', [`${real.headerPrefix}gateway`]: '1' } });
await request('page without parent', 'portNoParent', '/page', { headers: doc });
await request('page as fetch', 'port', '/page', { headers: { 'sec-fetch-dest': 'empty' } });
await request('fragment', 'port', '/fragment', { headers: { 'sec-fetch-dest': 'empty' } });
await request('echo', 'port', '/echo?q=1&r', { method: 'POST', headers: { 'content-type': 'text/plain', origin: SLOT, referer: `${SLOT}/page?x`, cookie: '__t_$TAG_a=1', 'x-forwarded-for': '1.2.3.4', forwarded: 'for=x', [`${real.headerPrefix}stand-session`]: '_oauth2_proxy=z', 'if-none-match': '"x"', 'accept-encoding': 'gzip, br' }, body: 'payload' });
await request('echo query only', 'port', '?a=b', {});
await request('big js br', 'port', '/big.js', { headers: { 'accept-encoding': 'gzip, deflate, br', 'sec-fetch-dest': 'script' } });
await request('big js gzip', 'port', '/big.js', { headers: { 'accept-encoding': 'gzip', 'sec-fetch-dest': 'script' } });
await request('big js plain', 'port', '/big.js', { headers: { 'sec-fetch-dest': 'script' } });
await request('gz css', 'port', '/gz.css', { headers: { 'accept-encoding': 'gzip', 'sec-fetch-dest': 'style' } });
await request('br json', 'port', '/br.json', { headers: { 'accept-encoding': 'br', 'sec-fetch-dest': 'empty' } });
await request('zstd kept', 'port', '/zstd.html', { headers: { ...doc, 'accept-encoding': 'zstd' } });
await request('image', 'port', '/img.png', { headers: { 'accept-encoding': 'gzip', 'sec-fetch-dest': 'image' } });
await request('plain small', 'port', '/plain.txt', { headers: { 'accept-encoding': 'gzip' } });
await request('no transform', 'port', '/nt.html', { headers: { ...doc, 'accept-encoding': 'gzip' } });
await request('json api', 'port', '/api/orders', {});
await request('missing', 'port', '/missing', {});
await request('no content', 'port', '/204', {});
await request('range', 'port', '/range', { headers: { range: 'bytes=0-3' } });
await request('head', 'port', '/page', { method: 'HEAD', headers: doc });
await request('redirect same', 'port', '/redirect-same', {});
await request('redirect absolute', 'port', '/redirect-abs', {});
await request('sse', 'port', '/sse', { headers: { 'sec-fetch-dest': 'empty' } });
await request('neighbour', 'port', '/__tk/p/$NPORT/remote.js', { headers: { 'sec-fetch-dest': 'script' } });
await request('neighbour bare', 'port', '/__tk/p/$NPORT', {});
await request('neighbour padded', 'port', '/__tk/p/0$NPORT/x', {});
await request('neighbour redirect', 'port', '/src/main.js', { headers: { 'sec-fetch-dest': 'script', referer: `${SLOT}/__tk/p/$NPORT/index.html` } });
await request('neighbour redirect doc', 'port', '/src/main.js', { headers: { 'sec-fetch-dest': 'document', referer: `${SLOT}/__tk/p/$NPORT/index.html` } });
await request('bridge script', 'port', '/__tk/bridge.js', {});
await request('devtools config', 'port', '/__tk/devtools/config', {});
await request('devtools config foreign origin', 'port', '/__tk/devtools/config', { headers: { origin: 'https://evil.example' } });
await request('devtools report', 'port', '/__tk/devtools/report', { method: 'POST', headers: { origin: SLOT, 'content-type': 'application/json' }, body: JSON.stringify({ events: [{ kind: 'console', level: 'error', source: 'console', text: `x ${SLOT}/__tk/p/$NPORT/a.js`, at: FIXED }] }) });
settings(SLOT, { blocked: ['big.js', '*/api/*'] });
await request('blocked script', 'port', '/big.js', { headers: { 'sec-fetch-dest': 'script' } });
await request('blocked doc', 'port', '/api/orders', { headers: doc });
await request('page with conditions', 'port', '/page', { headers: doc });
settings(SLOT, { blocked: [], throttle: { preset: 'offline', latencyMs: 0, downKbps: 0, upKbps: 0 } });
await request('offline doc', 'port', '/page', { headers: doc });
await request('offline fetch', 'port', '/api/orders', { headers: { 'sec-fetch-dest': 'empty' } });
settings(SLOT, { throttle: { preset: 'custom', latencyMs: 20, downKbps: 800, upKbps: 0 }, disableCache: true });
await request('throttled no-store', 'port', '/page', { headers: { ...doc, 'if-none-match': '"p1"', 'if-modified-since': 'x' } });
await request('throttled echo no-cache', 'port', '/echo', { headers: { 'if-none-match': '"p1"', 'if-modified-since': 'x' } });
settings(SLOT, { throttle: { preset: 'none', latencyMs: 0, downKbps: 0, upKbps: 0 }, disableCache: false });
await request('url page', 'url', '/page', { headers: { ...doc, 'accept-encoding': 'gzip' } });
await request('url echo', 'url', '/echo', { headers: { origin: URLSLOT, referer: `${URLSLOT}/p`, cookie: '__t_$UTAG_c=1', 'accept-encoding': 'br;q=0.5, gzip' } });
await request('url redirect same origin abs', 'url', '/redirect-site', {});
await request('url redirect foreign doc', 'url', '/redirect-foreign', { headers: doc });
await request('url redirect foreign fetch', 'url', '/redirect-foreign', { headers: { 'sec-fetch-dest': 'empty' } });
await request('url redirect invalid', 'url', '/redirect-invalid', { headers: doc });
await request('url redirect relative', 'url', '/redirect-same', {});
await request('stand page', 'stand', '/stand-page', { headers: { ...doc, cookie: '__t_$STAG_own=1; __t_$STAG__oauth2_proxy=planted', [`${real.headerPrefix}stand-session`]: '_oauth2_proxy=sess; other=1' } });
await request('stand echo', 'stand', '/echo', { headers: { cookie: '__t_$STAG_own=1', [`${real.headerPrefix}stand-session`]: '_oauth2_proxy=sess' } });
await request('stand sign-in doc', 'stand', '/stand-signin', { headers: doc });
await request('stand sign-in fetch', 'stand', '/stand-signin', { headers: { 'sec-fetch-dest': 'empty' } });
await request('stand oauth 401', 'stand', '/oauth2/sign_in', { headers: doc });
await request('stand asset first', 'stand', '/stand-asset.js', { headers: { 'sec-fetch-dest': 'script', 'accept-encoding': 'gzip' } });
await new Promise((r) => setTimeout(r, 200));
await request('stand asset hit', 'stand', '/stand-asset.js', { headers: { 'sec-fetch-dest': 'script', 'accept-encoding': 'gzip' } });
await request('stand asset 304', 'stand', '/stand-asset.js', { headers: { 'sec-fetch-dest': 'script', 'accept-encoding': 'gzip', 'if-none-match': 'W/"s1"' } });
await request('stand asset reload', 'stand', '/stand-asset.js', { headers: { 'sec-fetch-dest': 'script', 'accept-encoding': 'gzip', 'cache-control': 'no-cache' } });
await upgrade('ws', 'port', '/hmr', { origin: SLOT, cookie: '__t_$TAG_sid=1' });
await upgrade('ws neighbour', 'port', '/__tk/p/$NPORT/hmr', {});
await upgrade('ws rejected', 'port', '/ws-reject', {});
await upgrade('ws bad claim', null, '/hmr', {});
await upgrade('ws stand', 'stand', '/hmr', { cookie: '__t_$STAG_own=1', [`${real.headerPrefix}stand-session`]: '_oauth2_proxy=sess' });
settings(SLOT, { blocked: ['hmr'] });
await upgrade('ws blocked', 'port', '/hmr', {});
settings(SLOT, { blocked: [], throttle: { preset: 'offline', latencyMs: 0, downKbps: 0, upKbps: 0 } });
await upgrade('ws offline', 'port', '/hmr', {});
settings(SLOT, { throttle: { preset: 'none', latencyMs: 0, downKbps: 0, upKbps: 0 } });

const logs = {};
for (const slot of [SLOT, URLSLOT, STANDSLOT]) logs[slot] = store.readSlot(slot).network.map((e) => ({ ...e, durationMs: e.durationMs === null ? null : 0 }));
const out = { secret: SECRET, reserved: RESERVED, fixture: table, claims, steps, logs: JSON.parse(unfill(JSON.stringify(logs))) };
const target = process.env.GOLDEN_OUT ?? path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'proxy.json');
fs.writeFileSync(target, neutral(JSON.stringify(out, null, 1)) + '\n');
console.log('wrote', target, steps.length, 'steps');
process.exit(0);
