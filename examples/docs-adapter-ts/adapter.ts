// An example adapter of the documents protocol (docs/doc-adapters.md): documents and a board on example.com, kept in
// the memory of the process, in TypeScript with no dependencies. Node 22+ runs it as is:
//
//   ADAPTER_TOKEN=secret PORT=8080 node --experimental-strip-types adapter.ts
//
// A real adapter would sit in front of a corporate document service; the handlers below are the whole contract.
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { timingSafeEqual, randomBytes } from "node:crypto";

const HOST = "https://docs.example.com";
const adapterToken = process.env.ADAPTER_TOKEN ?? "";

const description = {
  protocol: "harnessly.docs/v1",
  name: "example-docs",
  version: "0.1.0",
  capabilities: ["read", "publish", "access", "comments", "boards"],
};

interface Version { n: number; text: string; format: string; at: string; note: string | null }
interface Doc { slug: string; title: string; versions: Version[]; isPublic: boolean; grants: { subject: string; role: string }[]; threads: any[] }

const now = () => new Date().toISOString();
const docs = new Map<string, Doc>();
function put(slug: string, title: string, format: string, text: string, note: string | null = null): Doc {
  const d = docs.get(slug) ?? { slug, title, versions: [], isPublic: false, grants: [], threads: [] };
  d.title = title;
  d.versions.push({ n: d.versions.length + 1, text, format, at: now(), note });
  docs.set(slug, d);
  return d;
}
put("handbook", "Handbook", "md", "# Handbook\nOur docs live at https://docs.example.com.\n");

const summary = (d: Doc) => ({
  slug: d.slug, kind: "artifact", title: d.title, url: `${HOST}/d/${d.slug}`, bound: false, owner: null, machineId: null,
  sourcePath: null, openThreads: d.threads.filter((t) => t.status === "open").length, unread: 0,
  lastActivityAt: d.versions[d.versions.length - 1].at,
});

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
const slugify = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 60) || "doc";

export const server = createServer(async (req, res) => {
  if (!authorized(req)) return void send(res, 401, { error: "unauthorized", message: "the adapter token is wrong" });
  const route = `${req.method} ${req.url}`;
  if (route === "GET /v1/describe") return void send(res, 200, description);
  let b: any;
  try {
    b = await readJSON(req);
  } catch {
    return void send(res, 400, { error: "bad_request", message: "the body is not JSON" });
  }
  const doc = b.slug ? docs.get(String(b.slug)) : undefined;
  const need = () => void send(res, 404, { error: "not_found" });

  switch (route) {
    case "POST /v1/list":
      return void send(res, 200, { docs: [...docs.values()].slice(0, Number(b.limit) || 100).map(summary) });
    case "POST /v1/get":
      return doc ? void send(res, 200, summary(doc)) : need();
    case "POST /v1/read": {
      if (!doc) return need();
      const v = b.version ? doc.versions.find((x) => String(x.n) === String(b.version)) : doc.versions[doc.versions.length - 1];
      return v ? void send(res, 200, { format: v.format, content: v.text, version: String(v.n) }) : need();
    }
    case "POST /v1/versions":
      if (!doc) return need();
      return void send(res, 200, {
        versions: [...doc.versions].reverse().map((v, i) => ({ n: v.n, sha: String(v.n), createdAt: v.at, author: null, note: v.note, current: i === 0 })),
      });
    case "POST /v1/diff": {
      if (!doc) return need();
      const a = doc.versions.find((x) => String(x.n) === String(b.from));
      const c = doc.versions.find((x) => String(x.n) === String(b.to));
      if (!a || !c) return need();
      const blocks = a.text === c.text ? [{ kind: "same", text: c.text }] : [{ kind: "removed", text: a.text }, { kind: "added", text: c.text }];
      return void send(res, 200, { from: String(a.n), to: String(c.n), blocks, stats: { added: 0, removed: 0 } });
    }
    case "POST /v1/embed":
      return doc ? void send(res, 200, { url: `${HOST}/embed/${doc.slug}` }) : need();
    case "POST /v1/publish": {
      if (typeof b.content !== "string" || !b.title) return void send(res, 400, { error: "bad_request", message: "title and content are required" });
      const slug = b.slug ? String(b.slug) : slugify(String(b.title));
      const d = put(slug, String(b.title), b.format === "html" ? "html" : "md", b.content, b.note || null);
      return void send(res, 200, { slug, version: d.versions.length, url: `${HOST}/d/${slug}` });
    }
    case "POST /v1/access":
      return doc ? void send(res, 200, { public: doc.isPublic, publicUrl: doc.isPublic ? `${HOST}/p/${doc.slug}` : null, grants: doc.grants }) : need();
    case "POST /v1/access/set":
      if (!doc) return need();
      if (typeof b.public === "boolean") doc.isPublic = b.public;
      if (Array.isArray(b.grants)) doc.grants = b.grants;
      return void send(res, 200, { public: doc.isPublic, publicUrl: doc.isPublic ? `${HOST}/p/${doc.slug}` : null, grants: doc.grants });
    case "POST /v1/threads":
      return doc ? void send(res, 200, { threads: doc.threads }) : need();
    case "POST /v1/threads/create": {
      if (!doc) return need();
      const id = randomBytes(8).toString("hex");
      const t = { id, externalId: null, slug: doc.slug, anchor: b.anchor, status: "open", createdBy: b.author, createdAt: now(), resolvedAt: null,
        comments: [{ id: randomBytes(8).toString("hex"), threadId: id, author: b.author, body: b.body, createdAt: now(), deliveredAt: null, deliveryError: null }] };
      doc.threads.push(t);
      return void send(res, 200, t);
    }
    case "POST /v1/threads/reply": {
      const t = doc?.threads.find((x) => x.id === b.threadId);
      if (!t) return need();
      t.comments.push({ id: randomBytes(8).toString("hex"), threadId: t.id, author: b.author, body: b.body, createdAt: now(), deliveredAt: null, deliveryError: null });
      return void send(res, 200, t);
    }
    case "POST /v1/threads/status": {
      const t = doc?.threads.find((x) => x.id === b.threadId);
      if (!t) return need();
      t.status = b.status;
      t.resolvedAt = b.status === "resolved" ? now() : null;
      return void send(res, 200, t);
    }
    case "POST /v1/threads/read":
      return void send(res, 200, {});
    case "POST /v1/boards":
      return void send(res, 200, { boards: [{ slug: "architecture", title: "Architecture", url: `${HOST}/b/architecture`, updatedAt: now() }] });
  }
  send(res, 404, { error: "not_found" });
});

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  server.listen(Number(process.env.PORT ?? 8080), "127.0.0.1");
}
