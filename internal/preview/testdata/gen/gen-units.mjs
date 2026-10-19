// The previous runtime's preview functions on generated inputs: URL parsing, claims, the SSRF guard, the rewrites, cache
// and coding headers, the pages, the dev server log lines, the ports parsing. Writes ../units.json (GOLDEN_OUT to write
// elsewhere, GOLDEN_SCALE to generate more cases).
//
//   OLD_RUNTIME_SRC=<checkout> NODE_MODULES=<its node_modules> PRODUCT=… PRODUCT_DOMAIN=… CLI=… \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen-units.mjs
import fs from 'node:fs';
import path from 'node:path';
import { createHmac } from 'node:crypto';
import { FIXED, rng, real, neutral } from './common.mjs';

const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};
process.env.GOLDEN_DATA_DIR = fs.mkdtempSync('/tmp/preview-golden-');
const SCALE = Number(process.env.GOLDEN_SCALE ?? 1);
const R = rng(20261019);

// The previous runtime names its modules after the product.
const lib = (rel) => import(`@/lib/${process.env.PRODUCT}-${rel}`);
const claim = await lib('preview/claim');
const ssrf = await lib('preview/ssrf');
const rewrite = await lib('preview/rewrite');
const cache = await lib('preview/cache-headers');
const enc = await lib('preview/encoding');
const asset = await lib('preview/asset-cache');
const pages = await lib('preview/pages');
const bridge = await lib('preview/bridge-script');
const lines = await lib('preview/devtools/server-log-lines');
const ports = await lib('preview/ports');
const resolve = await lib('preview/resolve');
const target = await lib('preview/devtools/target-url');
const standUrl = await lib('stand-url');
const standHost = await import(path.join(process.env.OLD_RUNTIME_SRC, 'deploy', process.env.PRODUCT, 'stand-host.mjs'));

const out = {};
const n = (k) => Math.max(1, Math.round(k * SCALE));

// URLs.
const hosts = ['example.com', 'EXAMPLE.com', 'a.b.example.org', '127.0.0.1', '0x7f.1', '2130706433', '0177.0.0.1', '[::1]', '[2001:DB8::1]', '[::ffff:1.2.3.4]',
  'localhost', 'xn--e1afmkfd.xn--p1ai', 'a_b.example', 'example.com.', '1.2.3.256', '999.1.1.1', '[1:2:3:4:5:6:7:8]', '[0:0:0:0:0:0:0:1]', 'exa%41mple.com', 'ex ample.com'];
const schemes = ['http', 'https', 'ws', 'wss', 'HTTP', 'ftp', 'file', 'mailto', 'javascript'];
const portsList = ['', ':80', ':443', ':0', ':8080', ':65535', ':65536', ':08080', ':x'];
const pathsList = ['', '/', '/a/b', '/a/../b', '/./x/', '/a b', '/é', '/%2e%2e/x', '/a?x=1 2', '/a#f g', '?q', '#h', '/a\\b', '//double', '/x/..', "/'q'", '/<x>', '/a?b=`c`#`d`'];
const urls = [];
for (let i = 0; i < n(400); i++) urls.push(`${R.pick(schemes)}://${R.bool(0.1) ? 'u:p@' : ''}${R.pick(hosts)}${R.pick(portsList)}${R.pick(pathsList)}${R.bool(0.2) ? R.pick(pathsList) : ''}`);
urls.push(' https://example.com/ ', 'https://example.com\t/a\nb', 'https:example.com', 'https:/example.com', 'not a url', '', 'https://', 'http://[::1', 'https://example.com:/x');
const view = (u) => ({ href: u.href, origin: u.origin, protocol: u.protocol, host: u.host, hostname: u.hostname, port: u.port, pathname: u.pathname, search: u.search, hash: u.hash, username: u.username, password: u.password });
out.urls = urls.map((raw) => { try { return { in: raw, out: view(new URL(raw)) }; } catch { return { in: raw, out: null }; } });
const refs = ['/x', 'x', '../y', '?q=1', '#h', '//other.example/p', 'https://example.com/z', 'http://example.com/z', 'HTTPS://Example.com:443/a', 'javascript:alert(1)', 'mailto:a@b', '', ' /sp ace', 'a/./b/../c', '/oauth2/start?rd=x', 'https://id.example.org/login', 'https://example.com:8443/', 'ftp://x/y'];
const bases = ['https://example.com/a/b?c=1#d', 'http://127.0.0.1:5173/', 'https://site.example.org/dir/'];
out.refs = [];
for (const base of bases) for (const ref of refs) {
  let res = null;
  try { res = view(new URL(ref, new URL(base))); } catch { res = null; }
  out.refs.push({ base, ref, out: res });
}

// Claims.
const SECRET = 's3cret-for-golden';
const b64 = (v) => Buffer.from(v).toString('base64url');
const sign = (payload, header = { alg: 'HS256' }, secret = SECRET) => {
  const head = `${b64(JSON.stringify(header))}.${b64(JSON.stringify(payload))}`;
  return `${head}.${createHmac('sha256', secret).update(head).digest('base64url')}`;
};
const now = Math.floor(FIXED / 1000);
const AUD = claim.PREVIEW_CLAIM_AUDIENCE;
const NEUTRAL_AUD = 'acme-preview-proxy';
// The previous runtime checks its own audience; the Go side gets the same payload with the neutral one.
const withAud = (payload, aud) => {
  const swap = (v) => (v === NEUTRAL_AUD ? aud : v);
  return { ...payload, ...(payload.aud === undefined ? {} : { aud: Array.isArray(payload.aud) ? payload.aud.map(swap) : swap(payload.aud) }) };
};
const goodPort = { sub: 'alice', target: { kind: 'port', port: 5173 }, slotOrigin: 'https://ai-alice-1.example.org', portSlots: { 5173: 'https://ai-alice-1.example.org', 4200: 'https://ai-alice-1.example.org/__tk/p/4200' }, parentOrigin: 'https://app.example.org', aud: 'acme-preview-proxy', exp: now + 60 };
const goodUrl = { sub: 'alice', target: { kind: 'url', origin: 'https://site.example.org' }, slotOrigin: 'https://ai-alice-2.example.org', portSlots: {}, aud: 'acme-preview-proxy', exp: now + 60 };
const variants = [
  {}, { aud: 'other' }, { aud: ['x', 'acme-preview-proxy'] }, { exp: now - 1 }, { exp: now }, { nbf: now + 10 }, { iat: 'x' }, { sub: '' }, { sub: 5 },
  { target: { kind: 'port', port: 0 } }, { target: { kind: 'port', port: 70000 } }, { target: { kind: 'port', port: 5173.5 } }, { target: { kind: 'port', port: '5173' } },
  { target: { kind: 'url', origin: 'https://site.example.org/' } }, { target: { kind: 'url', origin: 'https://SITE.example.org' } }, { target: { kind: 'url', origin: 'https://site.example.org:443' } },
  { target: { kind: 'url', origin: 'http://127.0.0.1:0' } }, { target: { kind: 'url', origin: 'http://[::1]:8080' } }, { target: { kind: 'url', origin: 'http://10.0.0.1' } },
  { slotOrigin: 'http://ai-alice-1.example.org' }, { slotOrigin: 'https://localhost' }, { portSlots: [] }, { portSlots: null },
  { portSlots: { 5173: 'https://ai-alice-1.example.org', 4200: 'https://evil.example.org/__tk/p/4200' } }, { portSlots: { 5173: 'https://ai-alice-1.example.org', '04200': 'https://ai-alice-1.example.org/__tk/p/04200' } },
  { portSlots: { 5174: 'https://ai-alice-1.example.org' } }, { parentOrigin: null }, { parentOrigin: 'http://app.example.org' }, { parentOrigin: 'https://app.example.org:8443' }, { parentOrigin: undefined },
];
out.claims = [];
for (const base of [goodPort, goodUrl]) for (const v of variants) {
  const payload = { ...base, ...v };
  for (const k of Object.keys(v)) if (v[k] === undefined) delete payload[k];
  if (base === goodUrl && v.portSlots === undefined && 'portSlots' in v) continue;
  const res = await claim.verifyPreviewClaim(sign(withAud(payload, AUD)), SECRET);
  out.claims.push({ token: sign(withAud(payload, NEUTRAL_AUD)), out: res });
}
for (const make of [(a) => sign(withAud(goodPort, a), { alg: 'HS512' }), (a) => sign(withAud(goodPort, a), { alg: 'HS256', crit: ['x'] }),
  (a) => sign(withAud(goodPort, a), { alg: 'HS256' }, 'other'), () => 'a.b.c', () => '', (a) => sign(withAud(goodPort, a)) + 'x', (a) => sign(withAud(goodPort, a), { alg: 'HS256', typ: 'JWT' })]) {
  out.claims.push({ token: make(NEUTRAL_AUD), out: await claim.verifyPreviewClaim(make(AUD), SECRET) });
}
out.claimSecret = SECRET;
out.claimAudience = NEUTRAL_AUD;
out.cookieTags = [{ kind: 'port', port: 5173 }, { kind: 'url', origin: 'https://site.example.org' }, { kind: 'port', port: 1 }].map((t) => ({ target: t, tag: claim.targetCookieTag(t, SECRET) }));

// SSRF.
const addrs = ['127.0.0.1', '10.1.2.3', '100.64.0.1', '100.128.0.1', '172.15.0.1', '172.16.0.1', '172.31.255.255', '192.168.1.1', '8.8.8.8', '1.1.1.1', '169.254.169.254', '0.0.0.0',
  '224.0.0.1', '255.255.255.255', '198.18.0.1', '203.0.113.5', '::1', '::', '::ffff:8.8.8.8', '::ffff:127.0.0.1', '64:ff9b::808:808', '2001:db8::1', '2001::1', '2002::1', 'fc00::1', 'fd12::1',
  'fe80::1', 'fe80::1%eth0', 'ff02::1', '2606:4700::6810:84e5', '2a00:1450:4001:82a::200e', 'FE80::1', '1.2.3', 'x', '', '01.2.3.4', '1.2.3.04', '::ffff:0:1.2.3.4', '100::1', '64:ff9b:1::1'];
out.forbidden = addrs.map((a) => ({ in: a, out: ssrf.isForbiddenAddress(a, new Set(['203.0.113.77', '2001:4860::1'])) }));
const lits = ['127.1', '0x7f.1', '2130706433', '0177.0.0.1', '1.2.3.4.', '[::1]', '[FE80::1]', '::1', 'example.com', '1.2.3.4.5', '256.1.1.1', '1.256.1.1', '0x', '0x.0x.0x.0x', '1.0x100', '4294967295', '4294967296', '00', '1..2', '09.1.1.1', ''];
out.literals = lits.map((h) => ({ in: h, out: ssrf.literalAddress(h), v4: ssrf.normalizeIPv4(h) }));
const dns = { 'pub.example': ['93.184.216.34'], 'mixed.example': ['93.184.216.34', '10.0.0.1'], 'v6.example': ['2606:4700::1'], 'both.example': ['2606:4700::1', '93.184.216.35'], 'none.example': [], 'self.example': ['203.0.113.77'] };
out.resolve = [];
for (const host of [...Object.keys(dns), 'missing.example', 'localhost', 'a.localhost', 'LOCALHOST.', '127.0.0.1', '[::1]', '8.8.8.8', '0x08080808', '203.0.113.77']) {
  const res = await ssrf.resolvePublic(host, {
    lookup: async (h) => { if (!(h in dns)) throw new Error('ENOTFOUND'); return dns[h].map((address) => ({ address, family: address.includes(':') ? 6 : 4 })); },
    localAddresses: new Set(['203.0.113.77']),
  });
  out.resolve.push({ host, out: res });
}
out.dns = dns;

// Rewrites.
const ctxPort = { slotOrigin: 'https://ai-alice-1.example.org', portSlots: { 5173: 'https://ai-alice-1.example.org', 4200: 'https://ai-alice-1.example.org/__tk/p/4200' } };
const ctxUrl = { slotOrigin: 'https://ai-alice-2.example.org', portSlots: {}, extraOrigins: { 'https://site.example.org': 'https://ai-alice-2.example.org' } };
const ctxUrlPort = { slotOrigin: 'https://ai-alice-3.example.org', portSlots: {}, extraOrigins: { 'http://site.example.org:8080': 'https://ai-alice-3.example.org' } };
const pieces = ['http://localhost:5173', 'https://localhost:5173/x', 'ws://127.0.0.1:4200', 'wss://0.0.0.0:4200/', 'http://[::1]:5173', 'http://localhost:51730', 'http://localhost:9999',
  'xhttp://localhost:5173', '_http://localhost:5173', 'http://localhost:4200"', 'http://localhost:4200/a', 'https://site.example.org', 'https://site.example.org/p', 'wss://site.example.org/ws',
  'https://site.example.org.evil.com', 'https://site.example.org-x.com', 'https://site.example.org:8443', 'http://site.example.org:8080/x', 'ws://site.example.org:8080', 'HTTP://localhost:5173',
  ' ', 'text ', 'é', '𝄞', '\n', '"', '=', '/', 'http://', 'localhost:', '5173', 'https://site.example.orgx', 'http://localhost:', '<a href="', '">'];
const texts = [];
for (let i = 0; i < n(200); i++) {
  let t = '';
  const k = 1 + R.int(8);
  for (let j = 0; j < k; j++) t += R.pick(pieces);
  texts.push(t);
}
out.rewrite = [];
for (const [name, ctx] of [['port', ctxPort], ['url', ctxUrl], ['urlport', ctxUrlPort]]) for (const text of texts) {
  out.rewrite.push({ ctx: name, text, out: rewrite.rewriteLocalOrigins(text, ctx) });
}
out.rewriteCtx = { port: ctxPort, url: ctxUrl, urlport: ctxUrlPort };
const streamOf = async (transform, chunks) => {
  const parts = [];
  transform.on('data', (c) => parts.push(Buffer.from(c)));
  const done = new Promise((r) => transform.on('end', r));
  for (const c of chunks) transform.write(c);
  transform.end();
  await done;
  return Buffer.concat(parts);
};
const chunked = (buf) => {
  const chunks = [];
  for (let i = 0; i < buf.length;) { const k = 1 + R.int(40); chunks.push(buf.subarray(i, i + k)); i += k; }
  return chunks;
};
out.streamRewrite = [];
for (let i = 0; i < n(80); i++) {
  const name = R.pick(['port', 'url']);
  let text = '';
  for (let j = 0; j < 20; j++) text += R.pick(pieces);
  const bytes = Buffer.from(text);
  if (R.bool(0.2)) bytes[R.int(bytes.length)] = 0xff;
  const chunks = chunked(bytes);
  const res = await streamOf(rewrite.createOriginRewriter(out.rewriteCtx[name]), chunks);
  out.streamRewrite.push({ ctx: name, chunks: chunks.map((c) => c.toString('base64')), out: res.toString('base64') });
}
const docs = ['<!doctype html><html><head></head><body>x</body></html>', '﻿<!DOCTYPE html>\n<html>', '  <!-- c --> <!doctype html><p>', '<!-- open', '<html><head>', '', 'plain text', '\xa0\xa0<!doctype x>',
  '<!--a--><!--b-->\n<!DOCTYPE html PUBLIC "x">rest', '<!doc', '<!DOCTYPE', '   ', '<!-->x-->'];
out.inject = [];
for (const doc of docs) for (let r = 0; r < 3; r++) {
  const bytes = Buffer.from(doc, 'latin1');
  const chunks = r === 0 ? [bytes] : chunked(bytes);
  const res = await streamOf(rewrite.createBridgeInjector('<script data-x></script>'), chunks);
  out.inject.push({ chunks: chunks.map((c) => c.toString('base64')), out: res.toString('base64') });
}
const big = Buffer.alloc(70 * 1024, 0x20);
out.inject.push({ chunks: chunked(big).map((c) => c.toString('base64')).slice(0, 4000), out: (await streamOf(rewrite.createBridgeInjector('<s>'), chunked(big))).toString('base64') });
out.inject.pop();
const headerSets = [
  { 'content-type': 'text/html', 'x-frame-options': 'DENY', 'content-security-policy': "default-src 'self'; frame-ancestors 'none'; script-src https://site.example.org" },
  { 'content-security-policy': 'frame-ancestors x', 'content-security-policy-report-only': "frame-ancestors 'self'; img-src *", location: '/next', 'clear-site-data': '"cookies"' },
  { location: 'http://localhost:4200/x', 'set-cookie': ['a=1; Domain=localhost; Secure; SameSite=Lax; Path=/', 'b=2; HttpOnly', `_oauth2_proxy=s; Domain=${real.standDomain}`], 'content-length': '10' },
  { location: 'https://site.example.org/back', 'set-cookie': [`_oauth2_proxy_1=z`] },
  { location: '//cdn.example/x' },
];
out.respHeaders = [];
for (const h of headerSets) for (const [name, ctx] of Object.entries(out.rewriteCtx)) for (const opts of [{ bodyRewritten: true, basePath: '', cookieTag: 'tag', stand: false }, { bodyRewritten: false, basePath: '/__tk/p/4200', cookieTag: 'tag', stand: true }]) {
  out.respHeaders.push({ ctx: name, headers: h, opts, out: rewrite.rewriteResponseHeaders(h, ctx, opts) });
}
out.cookies = [];
for (const header of ['', '__t_tag_a=1; b=2; __t_tag_c=3', '__t_tag_=x;__t_tagz_d=4', ' __t_tag_a = 1 ', '__t_other_a=1']) out.cookies.push({ kind: 'target', in: header, out: rewrite.upstreamCookie(header, 'tag') ?? null });
for (const v of ['a=1', 'a=1; Domain=x; secure; SameSite=Strict; Partitioned; Path=/', ' a=1 ;  HttpOnly ', 'a', 'a=1; domainx=1; Securely']) out.cookies.push({ kind: 'set', in: v, out: rewrite.renameSetCookie(v, 'tag') });
for (const [own, session] of [['a=1; _oauth2_proxy=p', '_oauth2_proxy=s; x=1'], [undefined, '_oauth2_proxy_0=a; _oauth2_proxy_0=b; _oauth2_proxy_1=c"d'], ['b', undefined], ['_oauth2_proxy', '']]) {
  out.cookies.push({ kind: 'stand', own: own ?? null, session: session ?? null, out: rewrite.standCookie(own, session) ?? null });
}

// Stands.
const standHosts = [`a${real.standDomain}`, `ai-x${real.standDomain}`, `auth${real.standDomain}`, `a-${real.standDomain}`, `x.y${real.standDomain}`, `ab${real.standDomain}.evil`, 'example.com', `${'a'.repeat(64)}${real.standDomain}`];
out.stands = standHosts.map((h) => ({ host: h, isHost: standHost.isStandHost(h), isOrigin: standHost.isStandOrigin(`https://${h}`), signIn: standUrl.standSignInUrl(`https://${h}/p/q?x=1 2&y=é`) }));
out.stands.push({ host: '', isHost: false, isOrigin: false, signIn: standUrl.standSignInUrl(`https://st${real.standDomain}/oauth2/callback`) });
out.signInRedirects = [];
for (const next of [`https://st${real.standDomain}/oauth2/start`, `https://st${real.standDomain}/page`, `https://${real.signIn[0]}/x`, `https://${real.signIn[1]}:8443/x`, `http://${real.signIn[0]}/x`, 'https://evil.example/x', `https://u:p@${real.signIn[2]}/`]) {
  out.signInRedirects.push({ origin: `https://st${real.standDomain}`, next, out: standUrl.isStandSignInRedirect(`https://st${real.standDomain}`, new URL(next)) });
}

// Cache and coding headers.
const ccs = ['', 'public, max-age=31536000, immutable', 'no-store', 'private, max-age=60', 's-maxage=10, max-age=5', 'no-cache', 'max-age="100"', 'max-age=abc', 'max-age=', 'MAX-AGE=10, Immutable',
  'stale-while-revalidate=30, max-age=10', 'proxy-revalidate, must-revalidate', 'no-transform', 'garbage;;, max-age=7', 'max-age=1 , max-age=2', 'max-age=0'];
const types = ['text/html; charset=utf-8', 'application/json', 'text/css', 'image/png', 'application/vnd.api+json', 'text/event-stream', 'font/woff2', 'application/javascript', 'text/plain', ''];
out.cacheControl = [];
for (const cc of ccs) for (const type of types) for (const document of [false, true]) {
  const h = { 'cache-control': cc, 'content-type': type };
  if (R.bool(0.3)) h.expires = 'Wed, 21 Oct 2026 07:28:00 GMT';
  out.cacheControl.push({ headers: h, document, out: cache.slotCacheControl(h, { document }) });
}
out.verdicts = [];
for (const cc of ccs) for (const type of types) for (const extra of [{}, { vary: 'Accept-Encoding' }, { vary: 'Cookie' }, { 'set-cookie': ['a=1'] }, { age: '100' }, { age: '99999999' }, { 'content-range': 'bytes 0-1/2' }]) {
  const h = { 'cache-control': cc, 'content-type': type, ...extra };
  out.verdicts.push({ method: 'GET', status: 200, headers: h, out: asset.storeVerdict('GET', 200, h) });
}
out.verdicts.push({ method: 'HEAD', status: 200, headers: { 'cache-control': ccs[1], 'content-type': 'image/png' }, out: asset.storeVerdict('HEAD', 200, { 'cache-control': ccs[1], 'content-type': 'image/png' }) });
out.allowsCache = [];
for (const h of [{}, { 'cache-control': 'no-cache' }, { pragma: 'no-cache' }, { range: 'bytes=0-1' }, { 'cache-control': 'max-age=0' }, { 'cache-control': 'No-Store' }]) for (const method of ['GET', 'POST']) {
  out.allowsCache.push({ method, headers: h, out: asset.requestAllowsCache(method, h) });
}
out.assetKey = asset.assetCacheKey('alice', 'br, gzip', 'https://site.example.org/a.js');
const accepts = ['', 'gzip', 'br', 'gzip, deflate, br', 'br;q=0.5, gzip;q=0.8', '*', '*;q=0', 'identity', 'gzip;q=0, *', 'deflate', 'BR, GZIP', 'gzip;q=1.2.3', 'br; q = 0.9 , gzip', 'gzip;level=1;q=0.3, br;q=0.3', ' , ,gzip'];
out.accept = accepts.map((a) => ({ in: a, accepted: enc.acceptedCodings(a), upstream: enc.upstreamAcceptEncoding(a), outgoing: enc.outgoingCoding(a) }));
out.codings = ['', 'identity', 'gzip', 'BR', ' deflate ', 'zstd', 'gzip, br'].map((c) => ({ in: c, out: enc.codingOf({ 'content-encoding': c }) }));
out.compressible = types.concat(['image/svg+xml', 'application/wasm', 'font/ttf', 'TEXT/HTML', 'application/x-font-ttf']).map((t) => ({ in: t, out: enc.isCompressible(t) }));
out.vary = [['', 'Accept-Encoding'], ['Origin', 'Accept-Encoding'], ['accept-encoding', 'Accept-Encoding'], ['*', 'Accept-Encoding'], [' a , ,b ', 'Accept-Encoding']].map(([v, name]) => ({ in: v, out: enc.withVary(v, name) }));
out.describe = [];
for (const h of [{ etag: '"x"', 'content-length': '5', vary: 'Origin' }, { etag: 'W/"y"', 'content-encoding': 'gzip' }, {}]) for (const [coding, recoded] of [[null, false], ['br', true], ['gzip', false], [null, true]]) {
  const copy = { ...h };
  enc.describeBody(copy, coding, recoded);
  out.describe.push({ headers: h, coding, recoded, out: copy });
}

// Pages.
out.pages = {
  status: [401, 403, 502].map((s) => ({ status: s, text: `x <b>&"'</b> ${s}`, out: pages.statusPage(s, `x <b>&"'</b> ${s}`) })),
  offer: [['https://other.example/a?b="c"', 'https://app.example.org'], ['https://other.example/', undefined]].map(([u, p]) => ({ url: u, parent: p ?? null, out: pages.offerPage(new URL(u), p, 'NONCE') })),
  signIn: [['https://st.example/oauth2/start?rd=x&y', 'https://app.example.org'], ['https://st.example/', undefined]].map(([u, p]) => ({ url: u, parent: p ?? null, out: pages.signInPage(u, p, 'NONCE') })),
  policy: pages.offerPolicy('NONCE'),
  bridgeTag: [['https://app.example.org', 'abc', undefined], ['https://app.example.org', 'a"b', '{"blocked":["<x>"]}']].map(([p, t, d]) => ({ parent: p, tag: t, devtools: d ?? null, out: bridge.bridgeTag(p, t, d) })),
};

// Dev server log lines.
const logPieces = ['ERROR in ./src/x.ts', 'error TS2304: Cannot find name', '    at foo (file.js:1:2)', '  > 12 | code', '     |   ^', 'warning: something', 'WARN deprecated',
  '✓ Compiled successfully', 'ready in 300ms', 'info line', '', '   ', '\x1b[31mERR!\x1b[0m bad', 'TypeError: x is undefined', 'Found 0 errors', '⚠ careful', 'Module not found: x',
  'normal output', '  src/a.ts:10:5', 'Application bundle generation complete.', 'fatal: no', 'Uncaught Error', '\tat bar (x:2:3)', 'line with trailing space   ', 'line\r\nwith crlf', 'deprecation notice'];
out.logLines = [];
for (let i = 0; i < n(150); i++) {
  const k = 1 + R.int(40);
  const parts = [];
  for (let j = 0; j < k; j++) parts.push(R.pick(logPieces));
  const text = parts.join(R.pick(['\n', '\r\n', '\r']));
  const limit = 1 + R.int(30);
  out.logLines.push({ text, limit, problems: lines.problemLines(text, limit), last: lines.lastLines(text, limit) });
}

// Ports.
const ss = [
  'LISTEN 0 511 127.0.0.1:5173 0.0.0.0:* users:(("node",pid=101,fd=20))',
  'LISTEN 0 511 [::1]:5173 [::]:* users:(("node",pid=101,fd=21))',
  'LISTEN 0 4096 0.0.0.0:22 0.0.0.0:*',
  'LISTEN 0 511 *:4200 *:* users:(("ng serve",pid=202,fd=3))',
  'LISTEN 0 511 [::]:3000 [::]:* users:(("next-server (v1",pid=303,fd=9))',
  'LISTEN 0 511 127.0.0.1:3000 0.0.0.0:* users:(("weird\\"name",pid=304,fd=9))',
  'ESTAB 0 0 127.0.0.1:5000 127.0.0.1:6000',
  'LISTEN 0 511 127.0.0.1:99999 0.0.0.0:*',
  'LISTEN 0 511 127.0.0.1:x 0.0.0.0:*',
  'LISTEN 0 1 127.0.0.1:18028 0.0.0.0:* users:(("runtime",pid=1,fd=1))',
  'LISTEN 0 1 127.0.0.1:8080 0.0.0.0:* users:(("python3",pid=404,fd=1))',
  'LISTEN 0 1 127.0.0.1:8081 0.0.0.0:* users:(("python3",pid=405,fd=1))',
  'garbage',
].join('\n');
const sockets = ports.parseListeningSockets(ss);
const cwds = { 101: '/work/app', 202: '/home/agent/wt/repo-0123abcd/task-1/sub', 303: '/elsewhere', 404: '/home/agent/wt/repo/task-2', 405: null };
const classified = await ports.classifyPorts(sockets, async (pid) => cwds[pid] ?? null, { workPaths: ['/work/app', '/work/other/'], worktreesRoot: '/home/agent/wt/', hiddenPorts: new Set([18028]) });
out.ports = { ss, sockets, cwds, classified, listener: [5173, 22, 18028, 7].map((p) => ({ port: p, out: ports.listenerOf(sockets, p, new Set([18028])) })) };
const rootsMap = { '/work/app/src/x.ts': '/work/app', '/work/app': '/work/app', '/home/agent': '/home/agent', '/': '/', '/home/agent/wt/repo': '/home/agent/wt/repo', '/home/agent/wt/repo/t1': '/home/agent/wt/repo/t1',
  '/work': '/work', '/work/app/': '/work/app/', '/tmp/x': '/tmp/x', '/home': '/home' };
out.normalise = [];
for (let i = 0; i < n(60); i++) {
  const paths = [];
  for (let j = 0; j < 1 + R.int(5); j++) paths.push(R.pick(Object.keys(rootsMap)));
  out.normalise.push({ paths, out: await ports.normaliseWorkRoots(paths, { resolveRoot: async (p) => rootsMap[p], worktreesRoot: '/home/agent/wt/', home: '/home/agent/' }) });
}
out.rootsMap = rootsMap;
out.search = [
  './src/a.ts\x0012:selector: \'x\'\nsrc/b:c.ts\x003:foo\nbad line\n\x001:x\n./d.ts\x00x:y\n./e.ts\x0099:z\n./f.ts\x001:w',
].map((o) => ({ in: o, out: resolve.parseSearchOutput(o, 3) }));
out.components = [{ name: 'App' }, { name: '1x' }, { name: 'A', hostTag: 'app-root' }, { name: 'A', hostTag: 'App' }, { name: 'A', source: { file: 'src/a.tsx', line: 3 } }, { name: 'A', source: { file: '', line: 1 } },
  { name: 'A', source: { file: 'x', line: 0 } }, { name: 'A', source: { file: 'x', line: 1.5 } }, { name: 'A', source: [] }, { name: 'A', source: null }, null, 'x', { name: '$a_b' }, { name: 'a'.repeat(101) }].map((c) => ({ in: c, out: resolve.parseComponentInput(c) }));
out.patterns = ['app-root', 'a.b', 'x$'].map((t) => ({ in: t, angular: resolve.angularSelectorPattern(t), react: resolve.reactDeclarationPattern(t) }));

// Slot addresses in reports.
const slot = 'https://ai-alice-1.example.org';
const targetTexts = [`${slot}/a`, `${slot}/__tk/p/4200/x`, `${slot}/__tk/p/04200/x`, `${slot}/__tk/p/123456/x`, `see ${slot}/__tk/p/5173 now`, `${slot}/__tk/p/5173:1:2`, `${slot}${slot}`, 'nothing', `${slot}/__tk/p/9x`, `${slot}/__tk/p/1'`];
out.targetText = [];
for (const t of targetTexts) for (const tg of [{ kind: 'port', port: 5173 }, { kind: 'url', origin: 'https://site.example.org' }, null]) out.targetText.push({ text: t, target: tg, slot, out: target.toTargetText(t, slot, tg) });

const target_ = process.env.GOLDEN_OUT ?? path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'units.json');
fs.writeFileSync(target_, neutral(JSON.stringify(out, null, 1)) + '\n');
console.log('wrote', target_);
