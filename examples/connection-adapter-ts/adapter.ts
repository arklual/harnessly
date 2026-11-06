// An example adapter of the connection protocol (docs/connection-adapters.md): an issue tracker on example.com, in
// TypeScript with no dependencies. Node 22+ runs it as is:
//
//   ADAPTER_TOKEN=secret PORT=8080 node --experimental-strip-types adapter.ts
//
// It holds no credentials of its own: the runtime sends the person's token, taken from its safe, in the body of each
// call, and the adapter checks it against the tracker. Here the "tracker" is a table in memory and any token that
// equals EXAMPLE_TRACKER_TOKEN (default "demo") is accepted.
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { timingSafeEqual } from "node:crypto";

const HOST = "tracker.example.com";
const adapterToken = process.env.ADAPTER_TOKEN ?? "";
const trackerToken = process.env.EXAMPLE_TRACKER_TOKEN ?? "demo";

const description = {
  protocol: "harnessly.connection/v1",
  name: "example-tracker",
  version: "0.1.0",
  capabilities: ["detect", "link", "issues"],
};

const issues = [
  { key: "EX-1", title: "Connect the tracker", status: { name: "Open", category: "todo" }, description: "A first issue." },
  { key: "EX-2", title: "Write the docs", status: { name: "Done", category: "done" }, description: "A second issue." },
].map((i) => ({ ...i, url: `https://${HOST}/issue/${i.key}`, project: "EX" }));

function send(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { "Content-Type": "application/json" }).end(JSON.stringify(body));
}

function authorized(req: IncomingMessage): boolean {
  if (!adapterToken) return true;
  const got = Buffer.from((req.headers.authorization ?? "").replace(/^Bearer /, ""));
  const want = Buffer.from(adapterToken);
  return got.length === want.length && timingSafeEqual(got, want);
}

async function readJSON(req: IncomingMessage): Promise<any> {
  const parts: Buffer[] = [];
  for await (const p of req) parts.push(p as Buffer);
  return parts.length ? JSON.parse(Buffer.concat(parts).toString()) : {};
}

export const server = createServer(async (req, res) => {
  if (!authorized(req)) return void send(res, 401, { error: "unauthorized", message: "the adapter token is wrong" });
  const route = `${req.method} ${req.url}`;
  if (route === "GET /v1/describe") return void send(res, 200, description);

  let body: any;
  try {
    body = await readJSON(req);
  } catch {
    return void send(res, 400, { error: "bad_request", message: "the body is not JSON" });
  }

  if (route === "POST /v1/detect") {
    let u: URL;
    try {
      u = new URL(body.url);
    } catch {
      return void send(res, 404, { error: "not_found" });
    }
    if (u.hostname !== HOST) return void send(res, 404, { error: "not_found" });
    return void send(res, 200, {
      baseUrl: `https://${HOST}`,
      deployment: "self-hosted",
      title: "Example tracker",
      methods: [{ id: "pat", recommended: true, fields: [{ id: "token", secret: true }] }],
      capabilities: ["issues"],
    });
  }

  if (route === "POST /v1/link") {
    const m = /\/issue\/([A-Z]+-\d+)/.exec(String(body.url));
    return void send(res, 200, m ? { found: true, kind: "issue", key: m[1] } : { found: false });
  }

  // every call below carries the credentials of the connection
  if (body.credentials?.token !== trackerToken) return void send(res, 401, { error: "denied", message: "the token is not accepted" });

  switch (route) {
    case "POST /v1/me":
      return void send(res, 200, { login: "demo", name: "Demo User" });
    case "POST /v1/projects":
      return void send(res, 200, { projects: [{ key: "EX", name: "Example", url: `https://${HOST}/project/EX` }] });
    case "POST /v1/issues/search": {
      const text = String(body.query?.text ?? "").toLowerCase();
      const found = issues.filter((i) => i.title.toLowerCase().includes(text)).map(({ description: _d, ...i }) => i);
      return void send(res, 200, { issues: found.slice(0, body.query?.limit ?? 50) });
    }
    case "POST /v1/issues/get": {
      const one = issues.find((i) => i.key === body.key);
      return void (one ? send(res, 200, one) : send(res, 404, { error: "not_found" }));
    }
  }
  send(res, 501, { error: "not_supported", message: `${route} is not available` });
});

if (process.argv[1]?.endsWith("adapter.ts")) {
  const port = Number(process.env.PORT ?? 8080);
  server.listen(port, () => console.log(`example-tracker adapter on :${port}`));
}
