#!/usr/bin/env node
// The standup backend: a stdio MCP server (newline-delimited JSON-RPC 2.0), no dependencies. Claude Code starts it
// from .mcp.json for the agent of the chat; it is not reachable from the view (hl.tools.call only calls https servers).
// Entries live in $STANDUP_DIR, else ./standup in the working directory.
import { createInterface } from 'node:readline';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Store } from './store.mjs';

const CARD = 'ui://harnessly.standup/card';
const dateProp = { type: 'string', description: 'YYYY-MM-DD; today when omitted' };

export const TOOLS = [
  {
    name: 'list_entries',
    description: "The day's standup entries as JSON. Each has updatedBy: 'person' means the person edited it in the window.",
    inputSchema: { type: 'object', properties: { date: dateProp } },
  },
  {
    name: 'save_entry',
    description: "Creates one teammate's update for the day, or changes the texts passed and keeps the others, in the teammate's own words. Refuses to empty an entry, and to change one the person edited unless expectedUpdatedAt is its current updatedAt.",
    inputSchema: {
      type: 'object',
      properties: {
        person: { type: 'string' },
        yesterday: { type: 'string' },
        today: { type: 'string' },
        blockers: { type: 'string' },
        expectedUpdatedAt: { type: 'string', description: "The entry's updatedAt from list_entries. Needed only to change an entry with updatedBy: 'person', after asking them" },
        date: dateProp,
      },
      required: ['person'],
    },
  },
  {
    name: 'show_standup',
    description: "Shows the day's entries as a card in the chat (the card reads them itself). Call it when the standup is done or the person asks to see it.",
    inputSchema: { type: 'object', properties: { date: dateProp } },
  },
];

/** What a tool returns: the data, plus for show_standup the view that draws it (MCP Apps: _meta.ui.resourceUri). */
export function callTool(store, name, args = {}) {
  switch (name) {
    case 'list_entries':
      return store.load(args.date);
    case 'save_entry': {
      const { date, ...entry } = args;
      return { saved: store.upsert(entry, date) };
    }
    case 'show_standup':
      return { date: store.load(args.date).date, _card: true };
    default:
      throw new Error(`unknown tool ${name}`);
  }
}

/**
 * What a tool answers with. show_standup names the view that draws the card (MCP Apps: _meta.ui.resourceUri). The
 * runtime finds the card in the agent's transcript, and there a result is only the first line of its text, cut at 100
 * characters: the text of this answer is therefore the whole card message on one short line, and the card view reads
 * the entries from the file itself instead of carrying them.
 */
function toolResult(data) {
  const { _card, ...body } = data;
  if (!_card) return { content: [{ type: 'text', text: JSON.stringify(body) }], structuredContent: body };
  const meta = { ui: { resourceUri: CARD } };
  return {
    content: [{ type: 'text', text: JSON.stringify({ _meta: meta, data: body }) }],
    structuredContent: body,
    _meta: meta,
  };
}

export function handle(store, msg) {
  const { id, method, params } = msg;
  if (id === undefined) return null; // a notification: no answer
  const ok = (result) => ({ jsonrpc: '2.0', id, result });
  const fail = (code, message) => ({ jsonrpc: '2.0', id, error: { code, message } });
  switch (method) {
    case 'initialize':
      return ok({
        protocolVersion: params?.protocolVersion ?? '2025-06-18',
        capabilities: { tools: {} },
        serverInfo: { name: 'standup', version: '0.1.0' },
      });
    case 'ping':
      return ok({});
    case 'tools/list':
      return ok({ tools: TOOLS });
    case 'tools/call': {
      if (!TOOLS.some((t) => t.name === params?.name)) return fail(-32602, `unknown tool ${params?.name}`);
      try {
        return ok(toolResult(callTool(store, params.name, params.arguments ?? {})));
      } catch (e) {
        // A refused input is a tool error the agent can read, not a broken protocol.
        return ok({ isError: true, content: [{ type: 'text', text: e.message }] });
      }
    }
    default:
      return fail(-32601, 'method not found');
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const store = new Store(resolve(process.env.STANDUP_DIR || 'standup'));
  createInterface({ input: process.stdin }).on('line', (line) => {
    if (line.trim() === '') return;
    let msg;
    try {
      msg = JSON.parse(line);
    } catch {
      return;
    }
    const res = handle(store, msg);
    if (res) process.stdout.write(`${JSON.stringify(res)}\n`);
  });
}
