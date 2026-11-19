// The person's connections through the SDK: the bridge checks the right and the arguments, the page asks the
// person before a change and before an agreement, and the answers reach the view as the SDK shapes them.
import { describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers, type IViewContext } from '@/next/features/app-views/bridge';
import { runConnection, type IConnectionIO } from '@/next/features/app-views/connections';
import { classicSdk, FrameRig, NONCE, settle, SLOT } from '@/next/features/app-views/frame-rig';
import { reaches } from '@/next/features/app-views/storage-events';

const context: IViewContext = {
  app: { id: 'acme.panel', version: '1' },
  target: 'side',
  view: 'main',
  theme: { mode: 'dark', tokens: {} },
  locale: 'ru',
  chat: null,
  user: null,
};

interface IHl {
  connections: {
    list: () => Promise<unknown>;
    tools: (c: string) => Promise<unknown>;
    call: (c: string, t: string, a?: object) => Promise<unknown>;
    api: (c: string, r: object) => Promise<{ status: number; ok: boolean; body: unknown }>;
    request: (c: string, o?: object) => Promise<unknown>;
  };
  on: (event: string, fn: (p: unknown) => void) => () => void;
  has: (name: string) => boolean;
}

const view = async (permissions: string[], handlers: IBridgeHandlers) => {
  const r = new FrameRig();
  const bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.panel', version: '1', permissions, targets: ['side'] },
    context,
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  r.bridge = bridge;
  const dom = r.load('<html><head></head><body></body></html>', NONCE, [classicSdk()]);
  const harnessly = (dom.window as unknown as { harnessly: { connect: (o?: object) => Promise<IHl> } })
    .harnessly;
  return { hl: await harnessly.connect(), bridge };
};

describe('hl.connections', () => {
  it('lists, reads tools and calls with the connection named by the view', async () => {
    const connections = vi.fn(async (op: string) => {
      if (op === 'list')
        return {
          connections: [
            { id: 'linear', name: 'Linear', status: 'ready', declared: 'read', access: 'read' },
            {
              id: 'yandex-mail',
              name: 'Яндекс Почта',
              status: 'needs_consent',
              declared: 'read',
              access: '',
            },
          ],
        };
      if (op === 'tools') return { tools: [{ name: 'list_issues', readOnly: true }] };
      return { content: [{ type: 'text', text: 'ok' }] };
    });
    const { hl } = await view(['connections.read'], { connections });
    const list = (await hl.connections.list()) as { id: string; access: string | null }[];
    // An agreement not given is null, never an empty string.
    expect(list.map((c) => [c.id, c.access])).toEqual([
      ['linear', 'read'],
      ['yandex-mail', null],
    ]);
    await expect(hl.connections.tools('linear')).resolves.toEqual([{ name: 'list_issues', readOnly: true }]);
    await expect(hl.connections.call('linear', 'list_issues', { q: 'a' })).resolves.toEqual({
      content: [{ type: 'text', text: 'ok' }],
    });
    expect(connections).toHaveBeenLastCalledWith('call', {
      connection: 'linear',
      tool: 'list_issues',
      args: { q: 'a' },
    });
    expect(hl.has('connections.list')).toBe(true);
  });

  it('is refused without the right, and nothing reaches the host', async () => {
    const connections = vi.fn();
    const { hl } = await view(['storage'], { connections });
    for (const run of [
      () => hl.connections.list(),
      () => hl.connections.tools('linear'),
      () => hl.connections.call('linear', 't'),
      () => hl.connections.api('linear', { path: '/x' }),
      () => hl.connections.request('linear'),
    ])
      await expect(run()).rejects.toMatchObject({ code: 'permission_denied' });
    expect(connections).not.toHaveBeenCalled();
    expect(hl.has('connections.list')).toBe(false);
  });

  it('checks the arguments before the host sees them', async () => {
    const connections = vi.fn(async () => ({}));
    const { hl } = await view(['connections.read'], { connections });
    await expect(hl.connections.call('', 'x')).rejects.toMatchObject({ code: 'invalid_params' });
    await expect(hl.connections.call('linear', 'x', 'nope' as unknown as object)).rejects.toMatchObject({
      code: 'invalid_params',
    });
    await expect(
      hl.connections.api('tracker', { method: 'TRACE' as 'GET', path: '/x' }),
    ).rejects.toMatchObject({ code: 'invalid_params' });
    await expect(hl.connections.api('tracker', { path: '' })).rejects.toMatchObject({
      code: 'invalid_params',
    });
    await expect(hl.connections.request('linear', { access: 'root' as 'all' })).rejects.toMatchObject({
      code: 'invalid_params',
    });
    expect(connections).not.toHaveBeenCalled();
  });

  it('parses a JSON answer of an API and keeps other text as text', async () => {
    const connections = vi.fn(async (_op: string, p: Record<string, unknown>) =>
      p.path === '/a'
        ? { status: 200, contentType: 'application/json; charset=utf-8', body: '{"n":1}' }
        : { status: 404, contentType: 'text/plain', body: 'no' },
    );
    const { hl } = await view(['connections.read'], { connections });
    await expect(hl.connections.api('tracker', { path: '/a', query: { q: 1 } })).resolves.toMatchObject({
      status: 200,
      ok: true,
      body: { n: 1 },
    });
    await expect(hl.connections.api('tracker', { path: '/b' })).resolves.toMatchObject({
      status: 404,
      ok: false,
      body: 'no',
    });
    expect(connections).toHaveBeenCalledWith('api', {
      connection: 'tracker',
      method: 'GET',
      path: '/a',
      params: { q: 1 },
    });
  });

  it('carries the refusal «ждёт вход» with the connection to sign in to', async () => {
    const { BridgeError } = await import('@/next/features/app-views/bridge');
    const connections = vi.fn(async () => {
      throw new BridgeError('auth_required', 'sign in', { signIn: { connection: 'yandex-mail' } });
    });
    const { hl } = await view(['connections.read'], { connections });
    await expect(hl.connections.call('yandex-mail', 'x')).rejects.toMatchObject({
      code: 'auth_required',
      signIn: { connection: 'yandex-mail' },
    });
  });

  it('hears that the connections changed', async () => {
    const { hl, bridge } = await view(['connections.read'], { connections: vi.fn() });
    const heard = vi.fn();
    hl.on('connections', heard);
    bridge.notify('harnessly/notifications/connections', {});
    await settle();
    expect(heard).toHaveBeenCalledTimes(1);
  });

  it('asks the person about a call of the agent as it does for the other loud calls, not for a list', async () => {
    const connections = vi.fn(async () => ({ connections: [] }));
    const elevate = vi.fn(async () => true);
    const { hl, bridge } = await view(['connections.read'], { connections, elevate });
    bridge.agentCall('t1');
    await hl.connections.list();
    await settle();
    expect(elevate).not.toHaveBeenCalled();
    await hl.connections.call('linear', 'list_issues', { a: 1 });
    await settle();
    expect(elevate).toHaveBeenCalledWith('harnessly/connections.call', 'linear: list_issues {"a":1}');
  });
});

describe('the page asks the person', () => {
  const io = (answers: Record<string, unknown>[], yes: boolean) => {
    const call = vi.fn(async (_body: Record<string, unknown>) => answers.shift() ?? {});
    const ask = vi.fn(async () => yes);
    return { call, ask } as IConnectionIO & { call: ReturnType<typeof vi.fn>; ask: ReturnType<typeof vi.fn> };
  };
  const change = { connection: 'linear', name: 'Linear', what: 'tool create_issue', detail: '{"title":"T"}' };

  it("repeats a change with the person's yes, and not without it", async () => {
    const yes = io([{ needsConfirm: change }, { result: { content: [] } }], true);
    await expect(
      runConnection('call', { connection: 'linear', tool: 'create_issue', args: {} }, yes),
    ).resolves.toEqual({ content: [] });
    expect(yes.ask).toHaveBeenCalledWith({ kind: 'confirm', change });
    expect(yes.call).toHaveBeenNthCalledWith(2, {
      connection: 'linear',
      tool: 'create_issue',
      args: {},
      op: 'call',
      confirmed: true,
    });

    const no = io([{ needsConfirm: change }], false);
    await expect(
      runConnection('call', { connection: 'linear', tool: 'create_issue' }, no),
    ).rejects.toMatchObject({ code: 'permission_denied' });
    expect(no.call).toHaveBeenCalledTimes(1);
  });

  it('gives a reading call at once, without a question', async () => {
    const rig = io([{ result: { content: [{ type: 'text', text: 'x' }] } }], true);
    await expect(runConnection('call', { connection: 'linear', tool: 'list_issues' }, rig)).resolves.toEqual({
      content: [{ type: 'text', text: 'x' }],
    });
    expect(rig.ask).not.toHaveBeenCalled();
  });

  it('keeps an agreement only when the person says yes in the card', async () => {
    const card = { connection: 'linear', name: 'Linear', access: 'read' };
    const yes = io([{ needsConsent: card }, { granted: true, access: 'read' }], true);
    await expect(runConnection('request', { connection: 'linear' }, yes)).resolves.toEqual({
      granted: true,
      access: 'read',
    });
    expect(yes.call).toHaveBeenLastCalledWith({ op: 'grant', connection: 'linear', access: 'read' });
    const no = io([{ needsConsent: card }], false);
    await expect(runConnection('request', { connection: 'linear' }, no)).resolves.toEqual({
      granted: false,
      access: '',
    });
    expect(no.call).toHaveBeenCalledTimes(1);
    // Already agreed to: no card.
    const done = io([{ granted: true, access: 'all' }], true);
    await runConnection('request', { connection: 'linear' }, done);
    expect(done.ask).not.toHaveBeenCalled();
  });
});

describe('the event of changed connections', () => {
  const change = { app: 'acme.panel', scope: 'connections', owner: '', key: '' };
  it("goes to the app's views that may read connections, whatever their storage", () => {
    expect(
      reaches(change, { app: { id: 'acme.panel', permissions: ['connections.read'] }, chat: null }),
    ).toBe(true);
    expect(reaches(change, { app: { id: 'acme.panel', permissions: ['storage'] }, chat: null })).toBe(false);
    expect(
      reaches(change, { app: { id: 'acme.other', permissions: ['connections.read'] }, chat: null }),
    ).toBe(false);
  });
});
