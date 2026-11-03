#!/usr/bin/env node
// The same standup tools over MCP's Streamable HTTP, with no dependencies: the one transport a VIEW can reach.
// hl.tools.call('standup.list_entries') goes from the machine's runtime to a server of the app's own .mcp.json, and only
// to `{"type": "http", "url": "https://..."}` on a public address: never to a stdio server, to localhost or to a LAN.
// This file is a stand-in for such a server (it is not part of the app: nothing in app.json or .mcp.json starts it).
//
//   PORT=8787 STANDUP_DIR=./standup STANDUP_TOKEN=secret node remote-server-example/server.mjs
//   claude -p --mcp-config '{"mcpServers":{"standup":{"type":"http","url":"http://127.0.0.1:8787/mcp"}}}' ...
//
// Stateless: every POST /mcp carries one JSON-RPC message and is answered at once with application/json (a request) or
// 202 (a notification). There is no session id, no event stream (GET answers 405), no batch (the 2025-06-18 revision
// dropped it).
import { createServer } from 'node:http';
import { resolve } from 'node:path';
import { handle } from '../backend/server.mjs';
import { Store } from '../backend/store.mjs';

const store = new Store(resolve(process.env.STANDUP_DIR || 'standup'));
const token = process.env.STANDUP_TOKEN || '';
const port = Number(process.env.PORT || 8787);
const MAX_BODY = 1 << 20;

const send = (res, status, body, headers = {}) => {
  res.writeHead(status, { 'content-type': 'application/json', ...headers });
  res.end(body === undefined ? undefined : JSON.stringify(body));
};

createServer((req, res) => {
  if (req.url.split('?')[0] !== '/mcp') return send(res, 404, { error: 'not found' });
  if (req.method !== 'POST') return send(res, 405, { error: 'POST only' }, { allow: 'POST' });
  // A server on a machine's own address must refuse a page that is not its own: a browser tab could otherwise drive it.
  const origin = req.headers.origin;
  if (origin && !/^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/.test(origin)) return send(res, 403, { error: 'origin not allowed' });
  if (token && req.headers.authorization !== `Bearer ${token}`) return send(res, 401, { error: 'unauthorized' });
  let raw = '';
  req.on('data', (chunk) => {
    raw += chunk;
    if (raw.length > MAX_BODY) req.destroy();
  });
  req.on('end', () => {
    let msg;
    try {
      msg = JSON.parse(raw);
    } catch {
      return send(res, 400, { jsonrpc: '2.0', id: null, error: { code: -32700, message: 'parse error' } });
    }
    if (typeof msg !== 'object' || msg === null || Array.isArray(msg)) {
      return send(res, 400, { jsonrpc: '2.0', id: null, error: { code: -32600, message: 'one JSON-RPC message per request' } });
    }
    const answer = handle(store, msg);
    if (answer === null) return send(res, 202); // a notification: accepted, nothing to say
    send(res, 200, answer);
  });
}).listen(port, '127.0.0.1', () => console.error(`standup MCP on http://127.0.0.1:${port}/mcp`));
