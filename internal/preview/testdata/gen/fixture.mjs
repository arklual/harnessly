// The dev server and the stand the proxy golden runs against, defined as data (fixture.json is written next to the
// golden) so the Go test serves exactly the same answers.
import zlib from 'node:zlib';

const page = (port, nport) => `<!doctype html><html><head><title>t</title></head><body><a href="http://localhost:${port}/x">x</a>
<script src="http://localhost:${nport}/remote.js"></script><script>new WebSocket('ws://localhost:${port}/hmr')</script></body></html>`;
const big = (port) => `/* ${'x'.repeat(3000)} */ fetch("http://127.0.0.1:${port}/api"); import("http://localhost:${port}/chunk.js");\n`;

// Every answer: status, header lines (name, value), body as text, and how the body is coded on the wire.
export const fixtureRoutes = (port, nport, standOrigin) => ({
  '/page': { status: 200, headers: [['Content-Type', 'text/html; charset=utf-8'], ['Set-Cookie', 'sid=1; Path=/; Domain=localhost; SameSite=Lax'], ['Set-Cookie', 'b=2; Secure'],
    ['ETag', '"p1"'], ['Cache-Control', 'max-age=60'], ['Content-Security-Policy', "default-src 'self'; frame-ancestors 'none'"], ['X-Frame-Options', 'DENY'], ['Last-Modified', 'Wed, 21 Oct 2026 07:28:00 GMT']], body: page(port, nport) },
  '/fragment': { status: 200, headers: [['Content-Type', 'text/html']], body: '<p>http://localhost:' + port + '/frag</p>' },
  '/big.js': { status: 200, headers: [['Content-Type', 'application/javascript'], ['Cache-Control', 'public, max-age=31536000, immutable'], ['ETag', '"b1"']], body: big(port) },
  '/gz.css': { status: 200, headers: [['Content-Type', 'text/css'], ['Content-Encoding', 'gzip'], ['Vary', 'Accept-Encoding']], body: `a{background:url(http://localhost:${port}/i.png)}`, coding: 'gzip' },
  '/br.json': { status: 200, headers: [['Content-Type', 'application/json'], ['Content-Encoding', 'br']], body: JSON.stringify({ api: `http://localhost:${port}/v1`, n: 'y'.repeat(2000) }), coding: 'br' },
  '/zstd.html': { status: 200, headers: [['Content-Type', 'text/html'], ['Content-Encoding', 'zstd']], body: 'opaque' },
  '/img.png': { status: 200, headers: [['Content-Type', 'image/png'], ['Cache-Control', 'max-age=86400']], body: 'P'.repeat(2048) },
  '/plain.txt': { status: 200, headers: [['Content-Type', 'text/plain'], ['Content-Length', '600']], body: 't'.repeat(600) },
  '/nt.html': { status: 200, headers: [['Content-Type', 'text/html'], ['Cache-Control', 'no-transform']], body: '<!doctype html><p>' + 'n'.repeat(2000) },
  '/api/orders': { status: 200, headers: [['Content-Type', 'application/json']], body: '{"orders":[]}' },
  '/missing': { status: 404, headers: [['Content-Type', 'text/plain']], body: 'nope' },
  '/204': { status: 204, headers: [], body: '' },
  '/range': { status: 206, headers: [['Content-Type', 'text/plain'], ['Content-Range', 'bytes 0-3/10']], body: 'rang' },
  '/redirect-same': { status: 302, headers: [['Location', '/page']], body: '' },
  '/redirect-abs': { status: 301, headers: [['Location', `http://localhost:${port}/page?a=1`]], body: '' },
  '/redirect-site': { status: 302, headers: [['Location', `http://site.example.org:${port}/page`]], body: '' },
  '/redirect-foreign': { status: 302, headers: [['Location', 'https://other.example/landing?x=1']], body: '' },
  '/redirect-invalid': { status: 302, headers: [['Location', 'javascript:alert(1)']], body: '' },
  '/sse': { status: 200, headers: [['Content-Type', 'text/event-stream'], ['Cache-Control', 'no-cache']], body: 'data: one\n\ndata: two\n\n' },
  '/stand-asset.js': { status: 200, headers: [['Content-Type', 'application/javascript'], ['Cache-Control', 'public, max-age=31536000, immutable'], ['ETag', '"s1"']], body: `console.log("${standOrigin}");` },
  '/stand-page': { status: 200, headers: [['Content-Type', 'text/html'], ['Set-Cookie', '_oauth2_proxy=leak; Path=/'], ['Set-Cookie', 'own=1']], body: `<!doctype html><a href="${standOrigin}/x">x</a>` },
  '/stand-signin': { status: 302, headers: [['Location', `${standOrigin}/oauth2/start?rd=%2F`]], body: '' },
  '/oauth2/sign_in': { status: 401, headers: [['Content-Type', 'text/html']], body: 'sign in' },
});

export const encodeBody = (route) => {
  const raw = Buffer.from(route.body);
  if (route.coding === 'gzip') return zlib.gzipSync(raw);
  if (route.coding === 'br') return zlib.brotliCompressSync(raw);
  return raw;
};
