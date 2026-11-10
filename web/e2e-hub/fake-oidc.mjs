// A fake OpenID Connect provider for the hub's end-to-end journey (HRN-761): no screen, no password. The authorize
// endpoint approves at once as the person named in ?login_hint=, else in the browser's fake_oidc_email cookie (each
// person of a test has a browser of their own, so one provider serves several people), else FAKE_OIDC_EMAIL, and sends
// the browser back with a code; the token endpoint answers with an RS256 id_token the hub checks against /jwks.
//
//   FAKE_OIDC_PORT=18590 FAKE_OIDC_CLIENT_ID=e2e FAKE_OIDC_CLIENT_SECRET=... node fake-oidc.mjs
import { createServer } from 'node:http';
import { createHash, createSign, generateKeyPairSync, randomBytes } from 'node:crypto';

const port = Number(process.env.FAKE_OIDC_PORT ?? 18590);
const base = process.env.FAKE_OIDC_ISSUER ?? `http://127.0.0.1:${port}`;
const clientId = process.env.FAKE_OIDC_CLIENT_ID ?? 'e2e';
const defaultEmail = process.env.FAKE_OIDC_EMAIL ?? 'sso-person@example.com';
const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
const jwk = { ...publicKey.export({ format: 'jwk' }), kid: 'e2e-1', use: 'sig', alg: 'RS256' };
const codes = new Map(); // code -> { nonce, email }
const tokens = new Map(); // access token -> email, for /userinfo

const b64 = (b) => Buffer.from(b).toString('base64url');
function sign(claims) {
  const head = b64(JSON.stringify({ alg: 'RS256', typ: 'JWT', kid: jwk.kid }));
  const body = b64(JSON.stringify(claims));
  const sig = createSign('RSA-SHA256').update(`${head}.${body}`).sign(privateKey);
  return `${head}.${body}.${b64(sig)}`;
}
const json = (res, status, obj) => {
  res.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store' });
  res.end(JSON.stringify(obj));
};
const cookie = (req, name) => {
  for (const part of (req.headers.cookie ?? '').split(';')) {
    const [k, ...v] = part.trim().split('=');
    if (k === name) return decodeURIComponent(v.join('='));
  }
  return '';
};
const form = (req) =>
  new Promise((ok) => {
    let s = '';
    req.on('data', (c) => (s += c));
    req.on('end', () => ok(new URLSearchParams(s)));
  });

createServer(async (req, res) => {
  const u = new URL(req.url, base);
  if (u.pathname === '/.well-known/openid-configuration')
    return json(res, 200, {
      issuer: base,
      authorization_endpoint: `${base}/authorize`,
      token_endpoint: `${base}/token`,
      userinfo_endpoint: `${base}/userinfo`,
      jwks_uri: `${base}/jwks`,
      response_types_supported: ['code'],
      subject_types_supported: ['public'],
      id_token_signing_alg_values_supported: ['RS256'],
      token_endpoint_auth_methods_supported: ['client_secret_basic', 'client_secret_post'],
      code_challenge_methods_supported: ['S256'],
    });
  if (u.pathname === '/jwks') return json(res, 200, { keys: [jwk] });
  if (u.pathname === '/authorize') {
    const code = randomBytes(12).toString('hex');
    codes.set(code, {
      nonce: u.searchParams.get('nonce') ?? '',
      email: u.searchParams.get('login_hint') || cookie(req, 'fake_oidc_email') || defaultEmail,
    });
    const back = new URL(u.searchParams.get('redirect_uri') ?? '');
    back.searchParams.set('code', code);
    back.searchParams.set('state', u.searchParams.get('state') ?? '');
    res.writeHead(302, { location: back.toString() });
    return res.end();
  }
  if (u.pathname === '/token' && req.method === 'POST') {
    const f = await form(req);
    const grant = codes.get(f.get('code') ?? '');
    if (!grant) return json(res, 400, { error: 'invalid_grant' });
    codes.delete(f.get('code'));
    const now = Math.floor(Date.now() / 1000);
    const sub = createHash('sha256').update(grant.email).digest('hex').slice(0, 24);
    const id = sign({
      iss: base,
      sub,
      aud: clientId,
      iat: now,
      exp: now + 600,
      nonce: grant.nonce,
      email: grant.email,
      email_verified: true,
      name: grant.email.split('@')[0],
    });
    const at = `at-${sub}-${randomBytes(6).toString('hex')}`;
    tokens.set(at, { sub, email: grant.email });
    return json(res, 200, { access_token: at, token_type: 'Bearer', expires_in: 600, id_token: id });
  }
  if (u.pathname === '/userinfo') {
    const who = tokens.get((req.headers.authorization ?? '').replace(/^Bearer /, ''));
    if (!who) return json(res, 401, { error: 'invalid_token' });
    return json(res, 200, { sub: who.sub, email: who.email, email_verified: true });
  }
  if (u.pathname === '/health') return json(res, 200, { ok: true });
  json(res, 404, { error: 'not_found' });
}).listen(port, '0.0.0.0', () => console.log(`fake oidc on ${base}`));
