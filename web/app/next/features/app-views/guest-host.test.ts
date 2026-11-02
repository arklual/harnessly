// The host script of a standalone app opened by link (internal/apps/sdk/guest-host.js), in a page of its own (jsdom)
// with a frame that answers the way a sandboxed view's does: the handshake with the nonce, the port it hands over,
// and what a guest's view may and may not ask of the host.
import { readFileSync } from 'node:fs';
import { MessageChannel, type MessagePort } from 'node:worker_threads';
import { JSDOM } from 'jsdom';
import { describe, expect, it, vi } from 'vitest';
import { settle } from '@/next/features/app-views/frame-rig';

const SRC = readFileSync(new URL('../../../../../internal/apps/sdk/guest-host.js', import.meta.url), 'utf8');
const NONCE = 'n0nce';

interface IReply {
  id?: number;
  result?: Record<string, unknown>;
  error?: { code: number; message: string; data: { code: string } };
}

const guestPage = (fetchImpl: (url: string, init: { body: string }) => Promise<unknown>) => {
  const cfg = JSON.stringify({
    nonce: NONCE,
    app: 'acme.demo',
    version: '1.2.0',
    view: 'main',
    write: false,
    locale: 'ru',
  });
  const dom = new JSDOM(
    `<!doctype html><html><head><meta name="hl-guest" content='${cfg}'></head><body><iframe id="view"></iframe></body></html>`,
    { runScripts: 'outside-only', url: 'https://machine.example/apps/guest/tok/' },
  );
  const win = dom.window as unknown as Record<string, unknown>;
  win.MessageChannel = MessageChannel;
  win.fetch = fetchImpl;
  win.matchMedia = () => ({ matches: false });
  const opened: unknown[][] = [];
  win.open = (...a: unknown[]) => opened.push(a);
  const posted: { data: Record<string, unknown>; origin: string; port: MessagePort | null }[] = [];
  const frame = dom.window.document.getElementById('view') as HTMLIFrameElement;
  const contentWindow = {
    postMessage: (data: Record<string, unknown>, origin: string, transfer: MessagePort[] = []) =>
      posted.push({ data, origin, port: transfer[0] ?? null }),
  };
  Object.defineProperty(frame, 'contentWindow', { value: contentWindow });
  dom.window.eval(SRC);
  const say = (data: unknown, source: unknown = contentWindow, origin = 'null') => {
    const event = new dom.window.Event('message');
    Object.defineProperties(event, {
      data: { value: data },
      source: { value: source },
      origin: { value: origin },
    });
    dom.window.dispatchEvent(event);
  };
  const hello = (nonce = NONCE) =>
    say({
      jsonrpc: '2.0',
      id: 'hl-shim-1',
      method: 'ui/initialize',
      params: { nonce, protocolVersion: '2025-11-21' },
    });
  return { dom, say, hello, posted, opened, contentWindow };
};

/** Calls over the port the host handed over, as the view's shim does. */
const over = (port: MessagePort) => {
  const got: IReply[] = [];
  port.on('message', (m: IReply) => got.push(m));
  return {
    got,
    call: async (id: number, method: string, params: Record<string, unknown> = {}) => {
      port.postMessage({ jsonrpc: '2.0', id, method, params });
      await settle(20);
      return got.find((m) => m.id === id) as IReply;
    },
  };
};

describe('guest host', () => {
  it('shakes hands with the view that shows the nonce, and hands over a port', async () => {
    const page = guestPage(async () => ({}));
    page.hello();
    await settle();
    expect(page.posted).toHaveLength(1);
    const { data, origin, port } = page.posted[0] as (typeof page.posted)[number];
    expect(origin).toBe('*');
    expect(port).not.toBeNull();
    const result = (
      data as {
        result: { hostContext: { harnessly: Record<string, unknown>; theme: string; locale: string } };
      }
    ).result;
    expect(result.hostContext.harnessly).toMatchObject({
      target: 'app',
      chat: null,
      user: null,
      guest: true,
      app: { id: 'acme.demo', version: '1.2.0' },
    });
    expect(result.hostContext.locale).toBe('ru');
    expect(result.hostContext.theme).toBe('light');
  });

  it('answers no one but its own frame, and no one without the nonce', async () => {
    const page = guestPage(async () => ({}));
    page.hello('wrong');
    page.say(
      { jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: { nonce: NONCE } },
      { postMessage: () => {} },
    );
    page.say(
      { jsonrpc: '2.0', id: 2, method: 'ui/initialize', params: { nonce: NONCE } },
      page.contentWindow,
      'https://evil.example',
    );
    await settle();
    expect(page.posted).toHaveLength(0);
    // A second hello, once there is a port, is not answered again.
    page.hello();
    await settle();
    page.hello();
    await settle();
    expect(page.posted).toHaveLength(1);
  });

  it('refuses a guest everything but the link: no chat, no tools, no panes, no documents', async () => {
    const page = guestPage(async () => ({}));
    page.hello();
    await settle();
    const rpc = over((page.posted[0] as (typeof page.posted)[number]).port as MessagePort);
    for (const method of [
      'ui/message',
      'tools/call',
      'harnessly/chat.read',
      'harnessly/pane.open',
      'harnessly/notify',
      'harnessly/flow.go',
      'harnessly/sinopia.document',
      'harnessly/data.set',
      'harnessly/ui.confirm',
    ]) {
      const answer = await rpc.call(10, method, { text: 'x' });
      expect(answer.error?.data.code, method).toBe('permission_denied');
      rpc.got.length = 0;
    }
  });

  it("keeps the guest storage in the link: every scope is the link's own area, over its route", async () => {
    const calls: { url: string; body: Record<string, unknown> }[] = [];
    const fetchImpl = vi.fn(async (url: string, init: { body: string }) => {
      const body = JSON.parse(init.body) as Record<string, unknown>;
      calls.push({ url, body });
      const reply = body.op === 'get' ? { value: { n: 1 } } : body.op === 'keys' ? { keys: ['a'] } : {};
      return {
        ok: body.op !== 'set',
        json: async () =>
          body.op === 'set' ? { error: 'permission_denied', message: 'this link only reads' } : reply,
      };
    });
    const page = guestPage(fetchImpl);
    page.hello();
    await settle();
    const rpc = over((page.posted[0] as (typeof page.posted)[number]).port as MessagePort);
    expect((await rpc.call(1, 'harnessly/storage.get', { scope: 'user', key: 'a' })).result).toEqual({
      value: { n: 1 },
    });
    expect((await rpc.call(2, 'harnessly/storage.keys', { scope: 'space' })).result).toEqual({ keys: ['a'] });
    // The runtime refuses a write on a link that only reads; the view sees the code.
    const refused = await rpc.call(3, 'harnessly/storage.set', { scope: 'install', key: 'a', value: 1 });
    expect(refused.error?.data.code).toBe('permission_denied');
    expect(calls.map((c) => c.url)).toEqual(['__guest/storage', '__guest/storage', '__guest/storage']);
    expect(calls[0]?.body).toEqual({ op: 'get', key: 'a' });
    expect((await rpc.call(4, 'harnessly/storage.get', { scope: 'user' })).error?.data.code).toBe(
      'invalid_params',
    );
  });

  it('opens an https link in a new window without the opener, and no other', async () => {
    const page = guestPage(async () => ({}));
    page.hello();
    await settle();
    const rpc = over((page.posted[0] as (typeof page.posted)[number]).port as MessagePort);
    expect((await rpc.call(1, 'ui/open-link', { url: 'https://example.org/x' })).result).toEqual({});
    expect(page.opened).toEqual([['https://example.org/x', '_blank', 'noopener,noreferrer']]);
    expect((await rpc.call(2, 'ui/open-link', { url: 'javascript:alert(1)' })).error?.data.code).toBe(
      'invalid_params',
    );
    expect(page.opened).toHaveLength(1);
  });
});
