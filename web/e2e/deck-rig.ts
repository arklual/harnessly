// What the deck tests share: five slides as an agent writes them, and the guest link's side: a stand-in of the gateway
// (it signs the runtime's public token the way the gateway does and serves the guest app with its config, the
// project's files and the published media) and a stand-in of the gateway's access routes the owner's page calls (the
// project's access, its share links), answered in the page.
import { createHmac, hkdfSync, randomBytes } from 'node:crypto';
import { createServer, request, type Server } from 'node:http';

import type { Page } from '@playwright/test';

import { freePort, type Runtime } from './runtime';

const TAG = 'harnessly-design';

export const SLIDES = [
  { title: 'Cover', head: 'Acme quarterly review', body: 'Where we are and where we go' },
  { title: 'Statement', head: 'One protocol for every service', body: 'Keys stay with the team' },
  { title: 'List one', head: 'What changed', body: 'Faster builds, fewer steps' },
  { title: 'List two', head: 'What is next', body: 'Exports, sharing, recording' },
  { title: 'Cards', head: 'Thank you', body: 'Questions welcome' },
];

export const slideFile = (s: (typeof SLIDES)[number]) => `<!doctype html>
<html><head><meta charset="utf-8"><script src="./support.js"></script></head>
<body>
<x-dc>
<helmet><link rel="stylesheet" href="deck-theme.css"><style>
html,body{margin:0}
.slide{position:relative;width:1920px;height:1080px;background:var(--deck-bg);color:var(--deck-ink);font-family:var(--deck-font)}
.slide>h1{position:absolute;left:134px;top:192px;width:1400px;font-size:88px;line-height:1.08;margin:0}
.slide>p{position:absolute;left:134px;top:480px;width:1200px;font-size:40px;margin:0;color:var(--deck-mute)}
</style></helmet>
<main class="slide">
  <h1 data-block="Title">${s.head}</h1>
  <p data-block="Body">${s.body}</p>
</main>
</x-dc>
</body></html>
`;

const ULID = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
export const pageId = (i: number) => `01JDECK0000000000000000${ULID[i + 1]}${ULID[i + 1]}Z`.slice(0, 26);

export interface IDeckGateway {
  /** The pair's secret of the runtime: write it to the runtime's AGENT_SECRET_FILE. */
  secret: string;
  /** The links the share popover made. */
  links: Record<string, unknown>[];
  guestComments: Record<string, unknown>[];
  /** The stand-in's origin, once started. */
  url: () => string;
  start: () => Promise<void>;
  stop: () => Promise<void>;
  stubAccess: (p: Page) => Promise<void>;
}

/** The stand-ins for the runtime and the project the test makes (both read when a request comes). */
export const deckGateway = (runtime: () => Runtime, project: () => string): IDeckGateway => {
  const secret = randomBytes(24).toString('hex');
  const links: Record<string, unknown>[] = [];
  const guestComments: Record<string, unknown>[] = [];
  let gateway: Server | null = null;
  let gatewayUrl = '';

  const b64url = (b: Buffer | string) => Buffer.from(b).toString('base64url');

  /** The token the gateway signs for a link's read of the project (HS256 under the derived key, 60 s). */
  const publicToken = (prj: string) => {
    const key = Buffer.from(hkdfSync('sha256', secret, 'harnessly', 'design-public/v1', 32));
    const now = Math.floor(Date.now() / 1000);
    const head = b64url(JSON.stringify({ alg: 'HS256' }));
    const body = b64url(JSON.stringify({ prj, aud: `${TAG}-public`, iat: now, exp: now + 60 }));
    const sig = b64url(createHmac('sha256', key).update(`${head}.${body}`).digest());
    return `${head}.${body}.${sig}`;
  };

  const runtimePath = (path: string) =>
    `/agents/api/platform/design-public/${encodeURIComponent(project())}${path}`;

  /** One request to the runtime's public read with a fresh token; the answer piped to `res` (or its body returned). */
  const fromRuntime = (path: string, headers: Record<string, string> = {}) =>
    new Promise<{ status: number; headers: Record<string, string | string[] | undefined>; body: Buffer }>(
      (ok, fail) => {
        const u = new URL(runtime().url);
        const req = request(
          {
            hostname: u.hostname,
            port: u.port,
            path: runtimePath(path),
            headers: { ...headers, 'x-harnessly-design-token': publicToken(project()) },
          },
          (res) => {
            const parts: Buffer[] = [];
            res.on('data', (c: Buffer) => parts.push(c));
            res.on('end', () =>
              ok({ status: res.statusCode ?? 0, headers: res.headers, body: Buffer.concat(parts) }),
            );
          },
        );
        req.on('error', fail);
        req.end();
      },
    );

  const startGateway = async () => {
    const port = await freePort();
    const server = createServer(async (req, res) => {
      const url = new URL(req.url ?? '/', 'http://gateway.test');
      const m = /^\/s\/([^/]+)(?:\/(app|f|m|comments|seen|index\.json)(\/.*)?)?$/.exec(url.pathname);
      if (!m) {
        res.statusCode = 404;
        res.end('not found');
        return;
      }
      const [, token, section, rest] = m;
      const link = links.find((l) => l.token === token);
      if (!link) {
        res.statusCode = 404;
        res.end('no link');
        return;
      }
      const pipe = async (path: string) => {
        const range = req.headers.range;
        const got = await fromRuntime(path, range ? { range } : {});
        const type = got.headers['content-type'];
        res.writeHead(got.status, {
          ...(typeof type === 'string' ? { 'Content-Type': type } : {}),
          ...(typeof got.headers['content-range'] === 'string'
            ? { 'Content-Range': got.headers['content-range'] }
            : {}),
          ...(typeof got.headers['accept-ranges'] === 'string'
            ? { 'Accept-Ranges': got.headers['accept-ranges'] }
            : {}),
          // The runtime gzips text even for `identity` (the gateway passes the coding on, as here).
          ...(typeof got.headers['content-encoding'] === 'string'
            ? { 'Content-Encoding': got.headers['content-encoding'] }
            : {}),
        });
        res.end(got.body);
      };
      if (section === 'app') return pipe(`/_app${rest ?? '/'}`);
      if (section === 'f') return pipe(rest ?? '/');
      if (section === 'm') return pipe(`/__media${rest ?? '/'}`);
      if (section === 'seen') {
        res.statusCode = 204;
        res.end();
        return;
      }
      if (section === 'comments') {
        res.setHeader('Content-Type', 'application/json');
        if (req.method === 'POST') {
          const parts: Buffer[] = [];
          for await (const c of req) parts.push(c as Buffer);
          const input = JSON.parse(Buffer.concat(parts).toString('utf8')) as Record<string, unknown>;
          const made = {
            id: guestComments.length + 1,
            ...input,
            at: new Date().toISOString(),
            resolved: false,
          };
          guestComments.push(made);
          res.statusCode = 201;
          res.end(JSON.stringify(made));
          return;
        }
        res.end(JSON.stringify({ comments: guestComments }));
        return;
      }
      const index = await fromRuntime('');
      if (section === 'index.json') {
        res.setHeader('Content-Type', 'application/json');
        res.end(index.body);
        return;
      }
      const shell = await fromRuntime('/_app/index.html');
      const html = shell.body.toString('utf8');
      const idx = JSON.parse(index.body.toString('utf8')) as Record<string, unknown>;
      const config = {
        v: 1,
        token,
        title: idx.title,
        rights: link.rights,
        expiresAt: null,
        view: link.view ?? null,
        kind: idx.kind ?? null,
        deck: idx.deck ?? null,
        boards: idx.boards,
        filesBase: `/s/${token}/f/`,
        commentsUrl: `/s/${token}/comments`,
      };
      const head = /<head(?:\s[^>]*)?>/i.exec(html);
      if (!head) throw new Error('the guest shell has no head');
      const at = head.index + head[0].length;
      const close = html.search(/<\/head\s*>/i);
      const data = `<script id="guest-config" type="application/json">${JSON.stringify(config).replace(/</g, '\\u003c')}</script>`;
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end(
        `${html.slice(0, at)}<base href="/s/${token}/app/">${html.slice(at, close)}${data}${html.slice(close)}`,
      );
    });
    gateway = server;
    await new Promise<void>((ok) => server.listen(port, '127.0.0.1', ok));
    gatewayUrl = `http://127.0.0.1:${port}`;
  };

  /** The gateway's access routes of the owner (the project's access, its share links), answered in the page. */
  const stubAccess = async (p: Page) => {
    await p.route('**/design/access/**', async (route) => {
      const req = route.request();
      const url = new URL(req.url());
      const json = (status: number, body: unknown) =>
        route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
      const path = url.pathname.replace(/\/$/, '');
      if (path.endsWith('/guest-comments')) return json(200, []);
      if (path.endsWith('/shares') && req.method() === 'GET') return json(200, links);
      if (path.endsWith('/shares') && req.method() === 'POST') {
        const input = (req.postDataJSON() ?? {}) as Record<string, unknown>;
        const token = randomBytes(12).toString('base64url');
        const link = {
          id: `l${links.length + 1}`,
          token,
          title: input.title ?? '',
          url: `${gatewayUrl}/s/${token}`,
          rights: input.rights ?? 'view',
          expiresAt: null,
          revokedAt: null,
          createdAt: new Date().toISOString(),
          opens: 0,
          comments: 0,
          view: input.view ?? null,
          pages: input.pages ?? null,
          version: input.version ?? 'latest',
        };
        links.push(link);
        return json(201, link);
      }
      const one = /\/shares\/([^/]+)$/.exec(path);
      if (one && req.method() === 'PATCH') {
        const link = links.find((l) => l.id === one[1]);
        if (!link) return json(404, { error: 'no link' });
        Object.assign(link, req.postDataJSON() ?? {});
        return json(200, link);
      }
      if (one && req.method() === 'DELETE') {
        const link = links.find((l) => l.id === one[1]);
        if (link) link.revokedAt = new Date().toISOString();
        return route.fulfill({ status: 204 });
      }
      if (path.endsWith('/log')) return json(200, []);
      return json(200, {
        role: 'owner',
        owner: 'ann@example.org',
        ownerName: 'Ann',
        team: 'none',
        canManage: true,
        editorsCanShare: false,
        request: null,
        people: [],
        invites: [],
        requests: [],
      });
    });
  };

  return {
    secret,
    links,
    guestComments,
    url: () => gatewayUrl,
    start: startGateway,
    stop: () => new Promise<void>((ok) => (gateway ? gateway.close(() => ok()) : ok())),
    stubAccess,
  };
};
