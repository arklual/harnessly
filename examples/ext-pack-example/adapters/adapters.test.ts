// The pack's own check: each adapter answers the frame (describe, token, idempotent replay) and its protocol's calls.
//   node --experimental-strip-types --test adapters/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Server } from "node:http";
import { connection } from "./connection.ts";
import { memory } from "./memory.ts";

async function serve(
  s: Server,
): Promise<{ url: string; close(): Promise<void> }> {
  await new Promise<void>((ok) => s.listen(0, "127.0.0.1", ok));
  const { port } = s.address() as { port: number };
  return {
    url: `http://127.0.0.1:${port}`,
    close: () => new Promise((ok) => s.close(() => ok())),
  };
}

const call = async (
  base: string,
  path: string,
  body?: unknown,
  headers: Record<string, string> = {},
) => {
  const r = await fetch(base + path, {
    method: body === undefined ? "GET" : "POST",
    headers: { "Content-Type": "application/json", ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return {
    status: r.status,
    replayed: r.headers.get("idempotent-replayed"),
    json: (await r.json()) as any,
  };
};

test("memory: the frame and the four calls", async () => {
  const file = join(mkdtempSync(join(tmpdir(), "pack-")), "memory.json");
  let a = await serve(memory(file, "s3cret"));
  const auth = { Authorization: "Bearer s3cret" };

  assert.equal((await call(a.url, "/v1/describe")).status, 401);
  const d = await call(a.url, "/v1/describe", undefined, auth);
  assert.equal(d.json.protocol, "harnessly.memory/v1");
  assert.deepEqual(d.json.capabilities, ["read", "write", "search"]);

  const put = await call(
    a.url,
    "/v1/put",
    { name: "plan.md", text: "# Plan\nship on friday\n" },
    auth,
  );
  assert.equal(put.status, 200);
  assert.equal(
    (
      await call(
        a.url,
        "/v1/put",
        { name: "plan.md", text: "x", ifVersion: "7" },
        auth,
      )
    ).status,
    409,
  );
  assert.equal(
    (await call(a.url, "/v1/put", { name: "../x", text: "x" }, auth)).status,
    400,
  );
  assert.equal(
    (await call(a.url, "/v1/get", { name: "plan.md" }, auth)).json.body,
    "# Plan\nship on friday",
  );
  assert.equal(
    (await call(a.url, "/v1/get", { name: "none.md" }, auth)).status,
    404,
  );
  assert.equal(
    (await call(a.url, "/v1/search", { query: "friday" }, auth)).json.hits[0]
      .name,
    "plan.md",
  );

  // the same Idempotency-Key is the first answer again
  const k = { ...auth, "Idempotency-Key": "k1" };
  const first = await call(
    a.url,
    "/v1/put",
    { name: "once.md", text: "one" },
    k,
  );
  const again = await call(
    a.url,
    "/v1/put",
    { name: "once.md", text: "two" },
    k,
  );
  assert.equal(again.replayed, "true");
  assert.equal(again.json.version, first.json.version);

  // a restart (an update of the host) keeps the notes
  await a.close();
  a = await serve(memory(file, "s3cret"));
  assert.equal((await call(a.url, "/v1/list", {}, auth)).json.notes.length, 3);
  await a.close();
});

test("connection: detect, link, credentials, issues and pages", async () => {
  const a = await serve(connection("s3cret", "demo"));
  const auth = { Authorization: "Bearer s3cret" };
  const ok = { credentials: { token: "demo" } };

  assert.equal(
    (await call(a.url, "/v1/describe", undefined, auth)).json.protocol,
    "harnessly.connection/v1",
  );
  assert.equal(
    (
      await call(
        a.url,
        "/v1/detect",
        { url: "https://gitlab.example.org/x" },
        auth,
      )
    ).status,
    404,
  );
  const det = await call(
    a.url,
    "/v1/detect",
    { url: "https://tracker.example.com/issue/EX-1" },
    auth,
  );
  assert.equal(det.json.baseUrl, "https://tracker.example.com");
  assert.deepEqual(
    (
      await call(
        a.url,
        "/v1/link",
        { url: "https://tracker.example.com/issue/EX-2" },
        auth,
      )
    ).json,
    { found: true, kind: "issue", key: "EX-2" },
  );

  assert.equal(
    (await call(a.url, "/v1/me", { credentials: { token: "wrong" } }, auth))
      .status,
    401,
  );
  assert.equal((await call(a.url, "/v1/me", ok, auth)).json.login, "demo");
  assert.equal(
    (
      await call(
        a.url,
        "/v1/issues/search",
        { ...ok, query: { text: "docs" } },
        auth,
      )
    ).json.issues[0].key,
    "EX-2",
  );
  assert.equal(
    (await call(a.url, "/v1/issues/get", { ...ok, key: "EX-9" }, auth)).status,
    404,
  );
  assert.equal(
    (await call(a.url, "/v1/pages/get", { ...ok, id: "1" }, auth)).json.body,
    "Welcome to Example Corp.",
  );
  assert.equal((await call(a.url, "/v1/nothing", {}, auth)).status, 501);
  await a.close();
});
