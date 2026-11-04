// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as core from './bridge-core';

const act = (fields: Record<string, unknown>) => {
  const parsed = core.parseParentMessage({ type: 'tk:act', id: 'a1', ...fields });
  if (!parsed || parsed.type !== 'tk:act') throw new Error(`not an act: ${JSON.stringify(fields)}`);
  return parsed;
};

const tool = (name: string, extra: Record<string, unknown> = {}) => ({
  name,
  description: `Tool ${name}`,
  execute: async () => 'ok',
  ...extra,
});

interface IContext {
  provideContext: (options: unknown) => void;
  clearContext: () => void;
  registerTool: (tool: unknown) => { unregister: () => void };
  unregisterTool: (name: string) => void;
}

describe('the modelContext shim', () => {
  let nav: { modelContext?: IContext };
  let registry: Map<string, core.ISiteTool>;
  const context = (): IContext => nav.modelContext as IContext;

  beforeEach(() => {
    nav = {};
    registry = core.makeSiteTools();
    core.installModelContext({ navigator: nav }, registry);
  });

  it('is defined when the browser has none, as an own enumerable configurable property', () => {
    const descriptor = Object.getOwnPropertyDescriptor(nav, 'modelContext');
    expect(descriptor?.configurable).toBe(true);
    expect(descriptor?.enumerable).toBe(true);
    for (const method of ['provideContext', 'clearContext', 'registerTool', 'unregisterTool'] as const)
      expect(typeof context()[method]).toBe('function');
  });

  it('registers, replaces by name and unregisters', () => {
    const handle = context().registerTool(tool('a', { annotations: { readOnlyHint: true } }));
    expect(registry.get('a')?.readOnly).toBe(true);
    context().registerTool(tool('a', { description: 'second' }));
    expect(registry.size).toBe(1);
    expect(registry.get('a')?.description).toBe('second');
    handle.unregister();
    expect(registry.has('a')).toBe(true);
    context().registerTool(tool('b'));
    context().unregisterTool('a');
    expect([...registry.keys()]).toEqual(['b']);
  });

  it('provideContext replaces the whole set, clearContext empties it', () => {
    context().registerTool(tool('old'));
    context().provideContext({ tools: [tool('x'), tool('y')] });
    expect([...registry.keys()]).toEqual(['x', 'y']);
    context().clearContext();
    expect(registry.size).toBe(0);
  });

  it('ignores invalid tools without throwing', () => {
    const bad: unknown[] = [
      null,
      'x',
      { name: 'no-exec' },
      { name: 'bad name', execute: () => 1 },
      { name: '', execute: () => 1 },
      { name: 'x'.repeat(129), execute: () => 1 },
      { name: 7, execute: () => 1 },
    ];
    for (const item of bad) expect(() => context().registerTool(item)).not.toThrow();
    expect(() => context().provideContext(null)).not.toThrow();
    expect(registry.size).toBe(0);
  });

  it('clips the description and keeps the schema as given', () => {
    const schema = { type: 'object' };
    context().registerTool(tool('a', { description: 'd'.repeat(2000), inputSchema: schema }));
    context().registerTool(tool('b', { description: 5 }));
    expect(registry.get('a')?.description).toHaveLength(1000);
    expect(registry.get('a')?.inputSchema).toBe(schema);
    expect(registry.get('b')?.description).toBe('');
  });

  it('wraps a native modelContext: the original runs and the tool is recorded', () => {
    const provideContext = vi.fn((_options: unknown) => undefined);
    const registerTool = vi.fn((_tool: unknown) => ({ unregister: vi.fn() }));
    const native = { registerTool, provideContext };
    const navigator = { modelContext: native };
    const seen = core.makeSiteTools();
    core.installModelContext({ navigator }, seen);
    const handle = navigator.modelContext.registerTool(tool('n') as never) as { unregister: () => void };
    navigator.modelContext.provideContext({ tools: [tool('p')] } as never);
    expect(provideContext).toHaveBeenCalledOnce();
    expect(registerTool).toHaveBeenCalledOnce();
    expect([...seen.keys()]).toEqual(['p']);
    expect(navigator.modelContext).toBe(native);
    expect(registerTool).not.toBe(native.registerTool);
    navigator.modelContext.registerTool(tool('q') as never);
    handle.unregister();
    expect(seen.has('q')).toBe(true);
  });
});

describe('tk:act parsing of tools', () => {
  it('takes tools and call with tool and input', () => {
    expect(act({ op: 'tools' })).toEqual({ type: 'tk:act', id: 'a1', op: 'tools' });
    expect(act({ op: 'call', tool: 'search.items:v-1', input: '{"q":1}' })).toEqual({
      type: 'tk:act',
      id: 'a1',
      op: 'call',
      tool: 'search.items:v-1',
      input: '{"q":1}',
    });
    expect(act({ op: 'call', tool: 'a', input: '' })).toEqual({
      type: 'tk:act',
      id: 'a1',
      op: 'call',
      tool: 'a',
    });
  });

  it('drops a bad name or too long input', () => {
    for (const fields of [
      { op: 'call', tool: 'bad name' },
      { op: 'call', tool: 'x'.repeat(129) },
      { op: 'call', tool: 5 },
      { op: 'call', tool: 'a', input: 'x'.repeat(8001) },
      { op: 'call', tool: 'a', input: 5 },
    ])
      expect(
        core.parseParentMessage({ type: 'tk:act', id: 'x', ...fields }),
        JSON.stringify(fields),
      ).toBeNull();
  });
});

describe('the agent using the page tools', () => {
  let bound: Record<string, unknown> & { siteTools: Map<string, core.ISiteTool> };
  let refs: core.IActRefs;
  const run = (fields: Record<string, unknown>) =>
    core.runAct(window, document, bound, core, refs, act(fields));
  const add = (name: string, extra: Record<string, unknown> = {}) => {
    const read = core.readSiteTool({ ...tool(name), ...extra });
    if (read) bound.siteTools.set(name, read);
  };

  beforeEach(() => {
    window.scrollBy = vi.fn();
    document.body.innerHTML = '<h1>Shop</h1>';
    bound = {
      ...core.captureNatives(window),
      origin: window.location.origin,
      parent: window,
      siteTools: core.makeSiteTools(),
    };
    refs = core.makeActRefs();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('refuses a call that would hand the tool a password or card details, and does not run it', async () => {
    const execute = vi.fn(() => 'ok');
    add('login', {
      execute,
      inputSchema: {
        type: 'object',
        properties: { email: { type: 'string' }, password: { type: 'string' } },
      },
    });
    add('pay', { execute });
    const bySchema = await run({ op: 'call', tool: 'login', input: '{"email":"a@b.c"}' });
    expect(bySchema.ok).toBe(false);
    expect(bySchema.text).toContain('(password): the person enters them');
    expect((await run({ op: 'call', tool: 'pay', input: '{"cardNumber":"4111"}' })).ok).toBe(false);
    expect(execute).not.toHaveBeenCalled();
  });

  it('says so when there are none', async () => {
    expect(await run({ op: 'tools' })).toEqual({
      ok: true,
      text: 'This page offers no tools of its own (WebMCP).',
    });
    expect((await run({ op: 'snapshot' })).text).not.toContain('WebMCP');
  });

  it('lists tools with read-only mark, one-line description and schema', async () => {
    add('search', {
      description: 'Find\n  items',
      annotations: { readOnlyHint: true },
      inputSchema: { type: 'object', properties: { q: { type: 'string' } } },
    });
    add('buy', { description: 'Buy' });
    const answer = await run({ op: 'tools' });
    expect(answer.ok).toBe(true);
    expect(answer.text).toBe(
      [
        'This page offers 2 tools of its own (WebMCP):',
        '- search (read-only): Find items',
        '  input: {"type":"object","properties":{"q":{"type":"string"}}}',
        '- buy: Buy',
      ].join('\n'),
    );
  });

  it('clips the schema and caps the list at 50', async () => {
    add('big', { inputSchema: { text: 'x'.repeat(1000) } });
    for (let i = 0; i < 54; i += 1) add(`t${i}`);
    const lines = (await run({ op: 'tools' })).text.split('\n');
    expect(lines[0]).toBe('This page offers 55 tools of its own (WebMCP):');
    expect(lines[2]).toHaveLength('  input: '.length + 600);
    expect(lines.filter((line) => line.startsWith('- '))).toHaveLength(50);
    expect(lines.at(-1)).toBe('…and 5 more');
  });

  it('adds a hint to the snapshot when the page has tools', async () => {
    add('a');
    add('b');
    expect((await run({ op: 'snapshot' })).text).toContain(
      '\nThis page offers 2 tools of its own (WebMCP): list them with browser_site_tools.',
    );
  });

  it('calls a tool and gives its content text', async () => {
    const execute = vi.fn(
      async (
        input: unknown,
        client: { requestUserInteraction: (cb: () => Promise<number>) => Promise<number> },
      ) => {
        expect(typeof client.requestUserInteraction).toBe('function');
        expect(await client.requestUserInteraction(async () => 7)).toBe(7);
        return {
          content: [
            { type: 'text', text: `got ${JSON.stringify(input)}` },
            { type: 'image', data: '..' },
            { type: 'text', text: 'end' },
          ],
        };
      },
    );
    add('go', { execute });
    const answer = await run({ op: 'call', tool: 'go', input: '{"n":1}' });
    expect(answer).toEqual({ ok: true, text: 'go returned:\ngot {"n":1}\n[image]\nend' });
    await run({ op: 'call', tool: 'go' });
    expect(execute.mock.calls[1]?.[0]).toEqual({});
  });

  it('formats string, other and cyclic results', async () => {
    add('s', { execute: () => 'plain' });
    add('o', { execute: () => ({ a: 1 }) });
    const loop: Record<string, unknown> = {};
    loop.self = loop;
    add('c', { execute: () => loop });
    expect((await run({ op: 'call', tool: 's' })).text).toBe('s returned:\nplain');
    expect((await run({ op: 'call', tool: 'o' })).text).toBe('o returned:\n{"a":1}');
    expect((await run({ op: 'call', tool: 'c' })).text).toBe('c returned:\n[object Object]');
  });

  it('clips a long result to 16000 chars', async () => {
    add('long', { execute: () => 'x'.repeat(20_000) });
    const answer = await run({ op: 'call', tool: 'long' });
    expect(answer.text).toBe(`long returned:\n${'x'.repeat(16_000)}`);
  });

  it('is not ok when the tool says isError', async () => {
    add('bad', { execute: () => ({ isError: true, content: [{ type: 'text', text: 'nope' }] }) });
    expect(await run({ op: 'call', tool: 'bad' })).toEqual({ ok: false, text: 'bad returned:\nnope' });
  });

  it('fails plainly on a throw, a rejection, an unknown tool, a missing name', async () => {
    add('t', {
      execute: () => {
        throw new Error('x'.repeat(600));
      },
    });
    add('r', { execute: () => Promise.reject(new Error('boom')) });
    expect(await run({ op: 'call', tool: 't' })).toEqual({
      ok: false,
      text: `The tool t failed: ${'x'.repeat(500)}`,
    });
    expect(await run({ op: 'call', tool: 'r' })).toEqual({ ok: false, text: 'The tool r failed: boom' });
    expect(await run({ op: 'call', tool: 'zz' })).toEqual({
      ok: false,
      text: 'The page has no tool zz; list them with browser_site_tools.',
    });
    expect(await run({ op: 'call' })).toEqual({ ok: false, text: 'call needs a tool name.' });
  });

  it('rejects input that is not a JSON object', async () => {
    const execute = vi.fn(async () => 'ok');
    add('a', { execute });
    for (const input of ['[1]', '5', 'null', '"s"', '{bad'])
      expect(await run({ op: 'call', tool: 'a', input })).toEqual({
        ok: false,
        text: 'input must be a JSON object.',
      });
    expect(execute).not.toHaveBeenCalled();
  });

  it('gives up on a tool that does not answer within 10 s', async () => {
    vi.useFakeTimers();
    bound = {
      ...core.captureNatives(window),
      origin: window.location.origin,
      parent: window,
      siteTools: core.makeSiteTools(),
    };
    add('slow', { execute: () => new Promise(() => undefined) });
    const pending = run({ op: 'call', tool: 'slow' });
    await vi.advanceTimersByTimeAsync(10_000);
    expect(await pending).toEqual({ ok: false, text: 'The tool slow did not answer within 10 s.' });
  });

  it('notes a change of address after the call', async () => {
    add('go', {
      execute: () => {
        window.history.pushState({}, '', '/after');
        return 'moved';
      },
    });
    const answer = await run({ op: 'call', tool: 'go' });
    expect(answer.text).toMatch(/^go returned:\nmoved; the page is now .*\/after$/);
  });
});

describe('site tools and secrets', () => {
  it('names a secret key in camel, snake and kebab case, and not ordinary ones', async () => {
    const { isSecretKey } = await import('./bridge-core');
    for (const key of [
      'password',
      'newPassword',
      'card_number',
      'cardNumber',
      'cvc',
      'smsOtp',
      'one-time-code',
      'client_secret',
      'pinCode',
    ])
      expect(isSecretKey(key), key).toBe(true);
    for (const key of ['passenger', 'passport', 'pin', 'cardId', 'eventId', 'query', 'company', 'spending'])
      expect(isSecretKey(key), key).toBe(false);
  });

  it('finds a secret at any depth of the arguments, and not in their values', async () => {
    const { secretInInput } = await import('./bridge-core');
    expect(secretInInput({ a: { b: { c: { d: { e: { f: [{ password: 'x' }] } } } } } })).toEqual({
      kind: 'secret',
      key: 'password',
    });
    expect(secretInInput({ q: 'my password is' })).toEqual({ kind: 'clean' });
  });

  it('walks a schema by its structure: fields under properties, items, anyOf and $defs, not keywords', async () => {
    const { secretInSchema } = await import('./bridge-core');
    const field = { type: 'string', description: 'password reset' };
    expect(secretInSchema({ properties: { email: field } })).toEqual({ kind: 'clean' });
    expect(secretInSchema({ properties: { user: { properties: { otp: field } } } })).toMatchObject({
      key: 'otp',
    });
    expect(secretInSchema({ properties: { list: { items: { properties: { cvc: field } } } } })).toMatchObject(
      { key: 'cvc' },
    );
    expect(
      secretInSchema({ anyOf: [{ properties: { email: field } }, { properties: { pinCode: field } }] }),
    ).toMatchObject({
      key: 'pinCode',
    });
    expect(secretInSchema({ $defs: { card: { properties: { cardNumber: field } } } })).toMatchObject({
      key: 'cardNumber',
    });
    expect(secretInSchema({ required: ['password'] })).toMatchObject({ key: 'password' });
  });

  it('counts a schema that throws or is too large as unchecked', async () => {
    const { secretInSchema } = await import('./bridge-core');
    const throwing = {
      get properties() {
        throw new Error('no');
      },
    };
    expect(secretInSchema(throwing)).toEqual({ kind: 'unchecked' });
    const wide = { anyOf: Array.from({ length: 6000 }, () => ({ type: 'string' })) };
    expect(secretInSchema(wide)).toEqual({ kind: 'unchecked' });
  });
});
