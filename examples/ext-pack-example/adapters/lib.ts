// What every adapter of the pack shares: the extension frame of docs/hub/extensions.md in one place. Node 22+, no
// dependencies. An adapter is `adapter({description, token, routes})`: it answers `GET /v1/describe`, refuses a wrong
// bearer token, parses the JSON body, and replays the first answer to a repeated Idempotency-Key.
import { createServer, type IncomingMessage, type Server } from "node:http";
import { timingSafeEqual } from "node:crypto";
import { existsSync, readFileSync, renameSync, writeFileSync } from "node:fs";

export interface Description {
  protocol: string;
  name: string;
  version: string;
  capabilities: string[];
}

// A handler answers [status, body]. A body of {error, message} is the frame's error shape.
export type Handler = (
  body: any,
  req: IncomingMessage,
) => [number, unknown] | Promise<[number, unknown]>;

export const fail = (
  status: number,
  error: string,
  message = "",
): [number, unknown] => [status, { error, message }];

function sameToken(req: IncomingMessage, token: string): boolean {
  if (!token) return true;
  const got = Buffer.from(
    (req.headers.authorization ?? "").replace(/^Bearer /, ""),
  );
  const want = Buffer.from(token);
  return got.length === want.length && timingSafeEqual(got, want);
}

async function readBody(req: IncomingMessage): Promise<any> {
  const parts: Buffer[] = [];
  for await (const p of req) parts.push(p as Buffer);
  return parts.length ? JSON.parse(Buffer.concat(parts).toString()) : {};
}

export function adapter(opts: {
  description: Description;
  token?: string;
  // "METHOD /path" -> handler
  routes: Record<string, Handler>;
}): Server {
  const replays = new Map<string, { status: number; text: string }>();
  return createServer(async (req, res) => {
    const send = (
      status: number,
      text: string,
      extra: Record<string, string> = {},
    ) =>
      void res
        .writeHead(status, { "Content-Type": "application/json", ...extra })
        .end(text);
    if (!sameToken(req, opts.token ?? ""))
      return send(
        401,
        JSON.stringify({
          error: "unauthorized",
          message: "the token is wrong or missing",
        }),
      );
    const route = `${req.method} ${req.url}`;
    if (route === "GET /v1/describe")
      return send(200, JSON.stringify(opts.description));
    const handler = opts.routes[route];
    if (!handler)
      return send(
        501,
        JSON.stringify({
          error: "not_supported",
          message: `${route} is not available`,
        }),
      );
    let body: unknown;
    try {
      body = await readBody(req);
    } catch {
      return send(
        400,
        JSON.stringify({
          error: "bad_request",
          message: "the body is not JSON",
        }),
      );
    }
    const key = req.headers["idempotency-key"];
    const seen =
      typeof key === "string" ? replays.get(`${route} ${key}`) : undefined;
    if (seen)
      return send(seen.status, seen.text, { "Idempotent-Replayed": "true" });
    const [status, out] = await handler(body, req);
    const text = JSON.stringify(out);
    if (typeof key === "string" && status < 500)
      replays.set(`${route} ${key}`, { status, text });
    send(status, text);
  });
}

// A JSON file that keeps an adapter's records across restarts (a real adapter has a database). Written by rename, so a
// stop in the middle of a write leaves the old file whole.
export function stateFile<T>(
  path: string | undefined,
  init: T,
): { value: T; save(): void } {
  const value: T =
    path && existsSync(path) ? JSON.parse(readFileSync(path, "utf8")) : init;
  return {
    value,
    save() {
      if (!path) return;
      writeFileSync(`${path}.tmp`, JSON.stringify(value));
      renameSync(`${path}.tmp`, path);
    },
  };
}

export function listen(server: Server, name: string): void {
  const port = Number(process.env.PORT ?? 8080);
  server.listen(port, process.env.HOST ?? "127.0.0.1", () =>
    console.log(`${name} adapter on :${port}`),
  );
}

export const isMain = (url: string): boolean =>
  !!process.argv[1] && url === new URL(`file://${process.argv[1]}`).href;
