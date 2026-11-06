// An example adapter of the memory protocol (docs/memory-adapters.md): the agent's notes for example.com, kept in the
// memory of the process, in TypeScript with no dependencies. Node 22+ runs it as is:
//
//   ADAPTER_TOKEN=secret PORT=8080 node --experimental-strip-types adapter.ts
//
// A real adapter would keep the notes in a wiki or a database; the six handlers below are the whole contract.
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { timingSafeEqual } from "node:crypto";

const adapterToken = process.env.ADAPTER_TOKEN ?? "";

const description = {
  protocol: "harnessly.memory/v1",
  name: "example-memory",
  version: "0.1.0",
  capabilities: ["read", "write", "search"],
};

interface Stored {
  text: string;
  modifiedMs: number;
  version: number;
}
const notes = new Map<string, Stored>([
  ["handbook.md", { text: "# Handbook\nOur docs live at https://docs.example.com.\n", modifiedMs: Date.now(), version: 1 }],
]);

const NAME = /^[A-Za-z0-9_.-]+\.md$/;

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

function parse(text: string): { title: string; description: string; body: string } {
  const front = /^---\n([\s\S]*?)\n---\n?/.exec(text);
  const body = (front ? text.slice(front[0].length) : text).trim();
  const heading = /^#\s*(.+)$/m.exec(body)?.[1]?.trim();
  const desc = front ? /^description:\s*(.+)$/m.exec(front[1])?.[1]?.trim() : undefined;
  return { title: heading ?? "", description: desc ?? "", body };
}

function entry(name: string, s: Stored) {
  const p = parse(s.text);
  return { name, title: p.title || name.replace(/\.md$/, ""), description: p.description, modifiedMs: s.modifiedMs, version: String(s.version) };
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

  if (route === "POST /v1/list") {
    return void send(res, 200, { notes: [...notes].sort(([a], [b]) => a.localeCompare(b)).map(([n, s]) => entry(n, s)) });
  }

  if (route === "POST /v1/get") {
    const s = notes.get(String(body.name));
    if (!s) return void send(res, 404, { error: "not_found" });
    return void send(res, 200, { ...entry(body.name, s), body: parse(s.text).body });
  }

  if (route === "POST /v1/put") {
    const name = String(body.name);
    if (!NAME.test(name) || typeof body.text !== "string") return void send(res, 400, { error: "bad_request", message: "bad name or text" });
    const cur = notes.get(name);
    if (cur && body.ifVersion && body.ifVersion !== String(cur.version)) {
      return void send(res, 409, { error: "conflict", message: "the note changed" });
    }
    const next = { text: body.text, modifiedMs: Date.now(), version: (cur?.version ?? 0) + 1 };
    notes.set(name, next);
    return void send(res, 200, entry(name, next));
  }

  if (route === "POST /v1/search") {
    const q = String(body.query ?? "").toLowerCase();
    const limit = Number(body.limit) > 0 ? Number(body.limit) : 20;
    const hits = [];
    for (const [name, s] of notes) {
      const line = s.text.split("\n").find((l) => l.toLowerCase().includes(q));
      if (q && line) hits.push({ ...entry(name, s), snippet: line.trim().slice(0, 200) });
      if (hits.length === limit) break;
    }
    return void send(res, 200, { hits });
  }

  send(res, 404, { error: "not_found" });
});

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  server.listen(Number(process.env.PORT ?? 8080), "127.0.0.1");
}
