import { describe, expect, it, vi } from 'vitest';
import { argsText, DECLINED, offerFor, pageOffer, reportsOf } from '@/next/features/app-views/page-tools';
import type { ConvId, MachineId } from '@/next/protocol/v2';

const sent = vi.hoisted(() => ({
  answers: [] as unknown[],
  acked: [] as string[],
  polls: 0,
  /** By chat: what the runtime hands out on each poll, and whether it hears acknowledgements. */
  handed: new Map<string, { ack: boolean; dead: boolean; lostAnswers: number; calls: unknown[][] }>(),
}));
vi.mock('@/next/core/net/runtime-json', async (actual) => ({
  ...(await actual<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: (_m: unknown, path: string, init?: RequestInit) => {
    const body = init?.body === undefined ? {} : (JSON.parse(String(init.body)) as Record<string, string>);
    if (path.includes('/apps/page-calls?')) {
      const conv = new URLSearchParams(path.slice(path.indexOf('?'))).get('conversation') ?? '';
      const scripted = sent.handed.get(conv);
      if (scripted) return Promise.resolve({ calls: scripted.calls.shift() ?? [], ack: scripted.ack });
      sent.polls += 1;
      const calls = sent.polls === 1 ? [{ id: 'c1', app: 'acme.a', tool: 'wipe', arguments: {} }] : [];
      return Promise.resolve({ calls });
    }
    if (path.endsWith('/apps/page-calls/ack')) {
      sent.acked.push(body.id);
      return Promise.resolve({ status: 'acked', dead: sent.handed.get(body.conversation)?.dead === true });
    }
    if (path.endsWith('/apps/page-calls/answer')) {
      const scripted = sent.handed.get(body.conversation);
      if (scripted && scripted.lostAnswers > 0) {
        scripted.lostAnswers -= 1;
        return Promise.reject(new Error('network'));
      }
      sent.answers.push(body);
    }
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

describe('a call is a command the page runs once (HRN-1117)', () => {
  const fill = { app: 'acme.a', tool: 'fill', arguments: { title: 'Jazz' } };

  it('acknowledges a command before it runs it, and does not run the same id coming again', async () => {
    vi.stubGlobal('window', globalThis);
    sent.handed.set('chat-ack', {
      ack: true,
      dead: false,
      lostAnswers: 0,
      calls: [[], [{ id: 'k1', ...fill }], [{ id: 'k1', ...fill }]],
    });
    const invoke = vi.fn();
    const offer = pageOffer('m1' as MachineId, 'chat-ack' as ConvId, 'f1', 'acme.a', invoke, () =>
      Promise.resolve(true),
    );
    offer.tools([{ name: 'fill', description: 'fills the form' }]);
    await vi.waitFor(() => expect(invoke).toHaveBeenCalledWith('k1', 'fill', { title: 'Jazz' }), {
      timeout: 4000,
    });
    expect(sent.acked).toContain('k1');
    // The command is handed out once more while the view works: the page says it has it and runs nothing.
    await vi.waitFor(() => expect(sent.acked.filter((id) => id === 'k1')).toHaveLength(2), { timeout: 4000 });
    expect(invoke).toHaveBeenCalledTimes(1);
    offer.result('k1', { result: { ok: true } });
    await vi.waitFor(() =>
      expect(sent.answers).toContainEqual(expect.objectContaining({ id: 'k1', result: { ok: true } })),
    );
    offer.drop();
    vi.unstubAllGlobals();
  });

  it('does not run a command the runtime calls dead', async () => {
    vi.stubGlobal('window', globalThis);
    sent.handed.set('chat-dead', {
      ack: true,
      dead: true,
      lostAnswers: 0,
      calls: [[], [{ id: 'd1', ...fill }]],
    });
    const invoke = vi.fn();
    const offer = pageOffer('m1' as MachineId, 'chat-dead' as ConvId, 'f1', 'acme.a', invoke, () =>
      Promise.resolve(true),
    );
    offer.tools([{ name: 'fill', description: 'fills the form' }]);
    await vi.waitFor(() => expect(sent.acked).toContain('d1'), { timeout: 4000 });
    expect(invoke).not.toHaveBeenCalled();
    expect(sent.answers).not.toContainEqual(expect.objectContaining({ id: 'd1' }));
    offer.drop();
    vi.unstubAllGlobals();
  });

  it('sends an answer again when it did not get through, and answers a command that comes back without running it', async () => {
    vi.stubGlobal('window', globalThis);
    sent.handed.set('chat-lost', {
      ack: true,
      dead: false,
      lostAnswers: 1,
      calls: [[], [{ id: 'l1', ...fill }], [], [], [], [], [{ id: 'l1', ...fill }]],
    });
    const invoke = vi.fn();
    const offer = pageOffer('m1' as MachineId, 'chat-lost' as ConvId, 'f1', 'acme.a', invoke, () =>
      Promise.resolve(true),
    );
    offer.tools([{ name: 'fill', description: 'fills the form' }]);
    await vi.waitFor(() => expect(invoke).toHaveBeenCalledTimes(1), { timeout: 4000 });
    offer.result('l1', { result: { ok: true } });
    const answersOf = () => sent.answers.filter((a) => (a as { id: string }).id === 'l1');
    await vi.waitFor(() => expect(answersOf()).toHaveLength(1), { timeout: 4000 });
    await vi.waitFor(() => expect(answersOf()).toHaveLength(2), { timeout: 9000 });
    expect(invoke).toHaveBeenCalledTimes(1);
    offer.drop();
    vi.unstubAllGlobals();
  }, 15_000);
});
