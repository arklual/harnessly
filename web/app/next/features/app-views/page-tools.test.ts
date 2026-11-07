import { describe, expect, it, vi } from 'vitest';
import { argsText, DECLINED, offerFor, pageOffer, reportsOf } from '@/next/features/app-views/page-tools';
import type { ConvId, MachineId } from '@/next/protocol/v2';

const sent = vi.hoisted(() => ({ answers: [] as unknown[], polls: 0 }));
vi.mock('@/next/core/net/runtime-json', async (actual) => ({
  ...(await actual<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: (_m: unknown, path: string, init?: RequestInit) => {
    if (path.includes('/apps/page-calls?')) {
      sent.polls += 1;
      const calls = sent.polls === 1 ? [{ id: 'c1', app: 'acme.a', tool: 'wipe', arguments: {} }] : [];
      return Promise.resolve({ calls });
    }
    if (path.endsWith('/apps/page-calls/answer')) sent.answers.push(JSON.parse(String(init?.body)));
    return Promise.resolve({});
  },
}));

const offer = (app: string, tools: string[], context?: unknown) => ({
  app,
  tools: tools.map((name) => ({ name, description: '' })),
  context,
  invoke: () => undefined,
  ask: () => Promise.resolve(true),
});

describe('page tools of a chat (HRN-808)', () => {
  it('reports one entry per app, the tools of all its frames once, and drops an app that offers nothing', () => {
    const r = reportsOf([
      offer('acme.a', ['fill']),
      offer('acme.a', ['fill', 'send'], { step: 2 }),
      offer('acme.b', []),
    ]);
    expect(r).toEqual([
      {
        app: 'acme.a',
        tools: [
          { name: 'fill', description: '' },
          { name: 'send', description: '' },
        ],
        context: { step: 2 },
      },
    ]);
  });

  it('sends a call to the newest frame of the app that offers the tool', () => {
    const offers: [string, ReturnType<typeof offer>][] = [
      ['f1', offer('acme.a', ['fill'])],
      ['f2', offer('acme.a', ['fill'])],
      ['f3', offer('acme.b', ['fill'])],
    ];
    expect(offerFor(offers, { app: 'acme.a', tool: 'fill' })?.[0]).toBe('f2');
    expect(offerFor(offers, { app: 'acme.a', tool: 'send' })).toBeNull();
  });

  it('runs a confirm tool only when the person agrees, and tells the agent a refusal', async () => {
    vi.stubGlobal('window', globalThis);
    const invoke = vi.fn();
    const ask = vi.fn(() => Promise.resolve(false));
    const offer = pageOffer('m1' as MachineId, 'chat-1' as ConvId, 'f1', 'acme.a', invoke, ask);
    offer.tools([{ name: 'wipe', description: 'wipe the list', confirm: true }]);
    await vi.waitFor(() =>
      expect(sent.answers).toContainEqual(expect.objectContaining({ id: 'c1', error: DECLINED })),
    );
    expect(ask).toHaveBeenCalledWith(expect.objectContaining({ name: 'wipe' }), '{}');
    expect(invoke).not.toHaveBeenCalled();
    offer.drop();
    vi.unstubAllGlobals();
  });

  it('shows the whole arguments of a call and refuses what it cannot show whole and plainly', () => {
    expect(argsText({ all: true })).toBe('{\n  "all": true\n}');
    expect(argsText({ note: 'x'.repeat(2000), drop: true })).toBeNull();
    expect(argsText({ 'a\u202eb': 1 })).toBeNull();
    expect(argsText({ ok: 'line\u2028next' })).toBeNull();
    expect(argsText({ ok: 'two\nlines' })).toBe('{\n  "ok": "two\\nlines"\n}');
    expect(argsText({ z: 'a\u200bb' })).toBeNull();
    expect(argsText({ z: 'a\ufeffb' })).toBeNull();
    expect(argsText({ z: 'ё — ok' })).not.toBeNull();
    const deep = JSON.parse('['.repeat(30) + ']'.repeat(30)) as unknown;
    expect(argsText({ deep })).toBeNull();
    expect(argsText({})).toBe('{}');
  });
});
