// The example app docs/apps/tracker-side, run in a view's frame with the real SDK and the real bridge: each state of the
// person's connection (none, no agreement, waiting for a sign-in, turned off, an answer, a refusal of the call itself) is
// shown in words, and the one button it has asks the person for the agreement and nothing else.
import { readFileSync } from 'node:fs';
import { describe, expect, it, vi } from 'vitest';
import {
  AppBridge,
  BridgeError,
  type IBridgeHandlers,
  type IViewContext,
} from '@/next/features/app-views/bridge';
import { classicSdk, FrameRig, NONCE, settle, SLOT } from '@/next/features/app-views/frame-rig';

const dir = new URL('../../../../../docs/apps/tracker-side/', import.meta.url);
const page = readFileSync(new URL('ui/index.html', dir), 'utf8').replace(/<script src[^>]*><\/script>/g, '');
const script = readFileSync(new URL('ui/main.js', dir), 'utf8');
const manifest = JSON.parse(readFileSync(new URL('harnessly/app.json', dir), 'utf8')) as {
  permissions: string[];
  targets: string[];
};

const context: IViewContext = {
  app: { id: 'harnessly.tracker-side', version: '0.1.0' },
  target: 'side',
  view: 'main',
  theme: { mode: 'light', tokens: {} },
  locale: 'ru',
  chat: null,
  user: null,
};

type TConnections = NonNullable<IBridgeHandlers['connections']>;

const open = async (connections: TConnections) => {
  const r = new FrameRig();
  const bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: {
      id: context.app.id,
      version: '0.1.0',
      permissions: manifest.permissions,
      targets: manifest.targets,
    },
    context,
    handlers: { connections },
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  r.bridge = bridge;
  const dom = r.load(page, NONCE, [classicSdk(), script]);
  await settle(80);
  const text = () => dom.window.document.getElementById('note')?.textContent ?? '';
  const buttons = () =>
    [...dom.window.document.querySelectorAll('#actions button')].map((b) => b.textContent);
  return { dom, bridge, text, buttons };
};

const conn = (status: string, access = '') => ({
  connections: [
    {
      id: 'yandex-tracker',
      name: 'Яндекс Трекер',
      kind: 'api',
      category: 'tasks',
      status,
      declared: 'read',
      access,
    },
  ],
});

describe('the tracker side widget', () => {
  it('asks for nothing but reading, in the sidebar', () => {
    expect(manifest.permissions).toEqual(['connections.read']);
    expect(manifest.targets).toEqual(['side']);
  });

  it('says the service is not connected', async () => {
    const { text, buttons } = await open(
      vi.fn(async () => ({
        connections: [
          {
            id: 'yandex-tracker',
            name: 'Яндекс Трекер',
            status: 'not_connected',
            declared: 'read',
            access: '',
          },
        ],
      })),
    );
    expect(text()).toContain('не подключён');
    expect(buttons()).toEqual([]);
  });

  it('says the person has not agreed, and asks for the agreement from the button only', async () => {
    const connections = vi.fn(async (op: string) => {
      if (op === 'list') return conn('needs_consent');
      if (op === 'request') return { granted: false, access: '' };
      throw new Error(`unexpected ${op}`);
    });
    const { text, buttons, dom } = await open(connections);
    expect(text()).toContain('нужно ваше согласие');
    expect(buttons()).toEqual(['Разрешить чтение']);
    // Nothing asked the person before the click.
    expect(connections.mock.calls.map((c) => c[0])).toEqual(['list']);
    (dom.window.document.querySelector('#actions button') as HTMLButtonElement).click();
    await settle(80);
    expect(connections).toHaveBeenCalledWith('request', { connection: 'yandex-tracker' });
    expect(text()).toContain('Без вашего согласия');
  });

  it('shows a service that waits for a sign-in, or is off', async () => {
    expect((await open(vi.fn(async () => conn('needs_signin', 'read')))).text()).toContain('ждёт входа');
    expect((await open(vi.fn(async () => conn('off', 'read')))).text()).toContain('выключено');
    expect((await open(vi.fn(async () => conn('unavailable', 'read')))).text()).toContain('не отвечает');
  });

  it('lists the open issues with one read request', async () => {
    const connections = vi.fn(async (op: string) =>
      op === 'list'
        ? conn('ready', 'read')
        : {
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify([
              { key: 'A-1', summary: 'Чинить', status: { display: 'Открыт' } },
              { key: 'A-2', summary: 'Писать' },
            ]),
          },
    );
    const { dom, text } = await open(connections);
    expect(text()).toBe('Открытых задач: 2');
    expect([...dom.window.document.querySelectorAll('.ts__key')].map((n) => n.textContent)).toEqual([
      'A-1',
      'A-2',
    ]);
    expect(connections).toHaveBeenLastCalledWith('api', {
      connection: 'yandex-tracker',
      method: 'POST',
      path: '/issues/_search',
      params: { perPage: 10 },
      body: { query: 'Assignee: me() Resolution: empty()' },
    });
  });

  it('says there is nothing to do when there are no issues', async () => {
    const { text } = await open(
      vi.fn(async (op: string) =>
        op === 'list' ? conn('ready', 'read') : { status: 200, contentType: 'application/json', body: '[]' },
      ),
    );
    expect(text()).toContain('нет');
  });

  it('turns a refusal of the call itself into the same words (the agreement taken back, a sign-in lost)', async () => {
    const refuse = (code: 'permission_denied' | 'auth_expired' | 'rate_limited') =>
      vi.fn(async (op: string) => {
        if (op === 'list') return conn('ready', 'read');
        throw new BridgeError(code, 'refused');
      });
    expect((await open(refuse('permission_denied'))).text()).toContain('нужно ваше согласие');
    expect((await open(refuse('auth_expired'))).text()).toContain('ждёт входа');
    expect((await open(refuse('rate_limited'))).text()).toContain('Слишком часто');
  });

  it('lists again when the connections change', async () => {
    let status = 'needs_consent';
    const connections = vi.fn(async (op: string) =>
      op === 'list'
        ? conn(status, status === 'ready' ? 'read' : '')
        : { status: 200, contentType: 'application/json', body: '[]' },
    );
    const { bridge, text } = await open(connections);
    expect(text()).toContain('согласие');
    status = 'ready';
    bridge.notify('harnessly/notifications/connections', {});
    await settle(80);
    expect(text()).toContain('нет');
  });

  it('puts what the service says into the page as text, never as markup', async () => {
    const evil = '<img src=x onerror=alert(1)>';
    const { dom } = await open(
      vi.fn(async (op: string) =>
        op === 'list'
          ? conn('ready', 'read')
          : {
              status: 200,
              contentType: 'application/json',
              body: JSON.stringify([{ key: evil, summary: evil }]),
            },
      ),
    );
    expect(dom.window.document.querySelector('img')).toBeNull();
    expect(dom.window.document.querySelector('.ts__summary')?.textContent).toBe(evil);
  });
});
