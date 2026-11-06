// An example adapter of the hub's extension protocol (docs/hub/extensions.md) in TypeScript, with no dependencies:
// Node 22+ runs it as is (`node --experimental-strip-types adapter.ts`).
//
//   ADAPTER_TOKEN=secret PORT=8080 node --experimental-strip-types adapter.ts
//
// It answers describe, checks the bearer token, and replays the answer of a call with an Idempotency-Key it has seen.
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { timingSafeEqual } from "node:crypto";

const description = {
  protocol: "harnessly.example/v1",
  name: "example-ts",
  version: "0.1.0",
  capabilities: ["echo"],
};
const token = process.env.ADAPTER_TOKEN ?? "";
const replays = new Map<string, { status: number; body: string }>();

function send(res: ServerResponse, status: number, body: unknown): string {
  const text = JSON.stringify(body);
  res.writeHead(status, { "Content-Type": "application/json" }).end(text);
  return text;
}

function authorized(req: IncomingMessage): boolean {
  if (!token) return true;
  const got = Buffer.from((req.headers.authorization ?? "").replace(/^Bearer /, ""));
  const want = Buffer.from(token);
  return got.length === want.length && timingSafeEqual(got, want);
}

async function readJSON(req: IncomingMessage): Promise<unknown> {
  const parts: Buffer[] = [];
  for await (const p of req) parts.push(p as Buffer);
  return parts.length ? JSON.parse(Buffer.concat(parts).toString()) : {};
}

export const server = createServer(async (req, res) => {
  if (!authorized(req)) return void send(res, 401, { error: "unauthorized", message: "the token is wrong or missing" });
  const route = `${req.method} ${req.url}`;

  if (route === "GET /v1/describe") return void send(res, 200, description);

  if (route === "POST /v1/echo") {
    // a call that changes something: the same Idempotency-Key gets the first answer again
    const key = req.headers["idempotency-key"];
    if (typeof key !== "string" || !key) {
      return void send(res, 400, { error: "idempotency_key_required", message: "send an Idempotency-Key" });
    }
    const seen = replays.get(key);
    if (seen) return void res.writeHead(seen.status, { "Content-Type": "application/json", "Idempotent-Replayed": "true" }).end(seen.body);
    let input: unknown;
    try {
      input = await readJSON(req);
    } catch {
      return void send(res, 400, { error: "bad_request", message: "the body is not JSON" });
    }
    const body = send(res, 200, { echo: input });
    replays.set(key, { status: 200, body });
    return;
  }

  send(res, 501, { error: "not_supported", message: `${route} is not available` });
});

if (process.argv[1]?.endsWith("adapter.ts")) {
  const port = Number(process.env.PORT ?? 8080);
  server.listen(port, () => console.log(`example-ts adapter on :${port}`));
}
