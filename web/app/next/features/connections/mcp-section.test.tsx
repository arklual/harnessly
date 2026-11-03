// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { ConnectionsPage } from '@/next/bindings/settings-connections';
import { setFetch } from '@/next/core/net/http';
import {
  readAuthStart,
  readMcpCatalogEntry,
  readMcpProbe,
  readMcpServer,
} from '@/next/features/connections/mcp-api';
import { headerValue, openCatalog } from '@/next/features/connections/mcp-model';
import McpSection, { type IMcpSectionProps } from '@/next/features/connections/mcp-section';
import { pinLocale } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

beforeAll(() => {
  pinLocale('en');
  // The section's animations ask about reduced motion; jsdom has no matchMedia.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  setFetch((url, init) => fetch(url, init));
});

// The machine's path may carry a query (which machine): requests are told apart by their path alone.
const pathOf = (url: string) => new URL(url, 'http://page.test').pathname;

const MACHINE = 'local' as MachineId;
const TOKEN = 'ghp_' + 'x9'.repeat(18);
const AUTH_URL = 'https://mcp.linear.app/authorize?client_id=c1&state=s1';

// A Server as GET <platform>/mcp answers it (internal/mcpconn/model.go).
const server = (over: Record<string, unknown> = {}) => ({
  id: 'm1',
  name: 'Linear',
  slug: 'linear',
  url: 'https://mcp.linear.app/mcp',
  transport: 'http',
  auth: 'oauth',
  catalogId: 'linear',
  enabled: true,
  state: 'ok',
  me: 'anna@acme.com',
  tools: [
    { name: 'list_issues', description: 'List issues of a team' },
    { name: 'create_issue', description: 'Create an issue' },
  ],
  createdAt: '2026-11-03T10:00:00Z',
  ...over,
});

const linear = {
  id: 'linear',
  name: 'Linear',
  url: 'https://mcp.linear.app/mcp',
  transport: 'http',
  auth: 'oauth',
  docsUrl: 'https://linear.app/docs/mcp',
  blurb: 'Issues, projects and cycles',
  blurbRu: 'Задачи, проекты и циклы',
};
const github = {
  id: 'github',
  name: 'GitHub',
  url: 'https://api.githubcopilot.com/mcp/',
  transport: 'http',
  auth: 'header',
  headerName: 'Authorization',
  tokenUrl: 'https://github.com/settings/personal-access-tokens/new',
  docsUrl: 'https://github.com/github/github-mcp-server',
  blurb: 'Repositories, issues and pull requests',
  blurbRu: 'Репозитории, задачи и пул-реквесты',
};

const catalog = [readMcpCatalogEntry(linear), readMcpCatalogEntry(github)];

const props = (over: Partial<IMcpSectionProps> = {}): IMcpSectionProps => ({
  mcp: { servers: [], catalog },
  onProbe: vi.fn(async () => ({
    probe: readMcpProbe({ url: 'https://mcp.acme.dev/mcp', transport: 'http', auth: 'oauth', name: 'Acme' }),
  })),
  onAdd: vi.fn(async () => ({
    server: readMcpServer(server({ state: 'signin', tools: [], me: '' })),
    authStart: readAuthStart({ auth: 'oauth' }),
  })),
  onSignIn: vi.fn(async () => ({ authUrl: AUTH_URL })),
  onRefresh: vi.fn(async () => undefined),
  onHeader: vi.fn(async () => null),
  onCheck: vi.fn(async () => null),
  onRename: vi.fn(async () => null),
  onEnable: vi.fn(async () => null),
  onRemove: vi.fn(async () => null),
  ...over,
});

/** A popup as window.open gives it: blank at first, sent to the sign-in later. */
const fakePopup = () => ({ closed: false, close: vi.fn(), location: { href: 'about:blank' } });

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

describe('the MCP readers', () => {
  it('read a server without secrets, and the catalog without what is connected', () => {
    const s = readMcpServer(server());
    expect(s.tools).toHaveLength(2);
    expect(s.headerName).toBe('');
    expect(readMcpServer(server({ transport: undefined })).transport).toBe('http');
    expect(() => readMcpServer(server({ auth: 'magic' }))).toThrow();
    expect(openCatalog(catalog, [s]).map((e) => e.id)).toEqual(['github']);
  });

  it('puts «Bearer» before a bare token in Authorization only', () => {
    expect(headerValue('Authorization', ` ${TOKEN} `)).toBe(`Bearer ${TOKEN}`);
    expect(headerValue('authorization', `Token ${TOKEN}`)).toBe(`Token ${TOKEN}`);
    expect(headerValue('X-API-Key', TOKEN)).toBe(TOKEN);
  });
});

describe('Settings → Connections → MCP servers', { timeout: 20_000 }, () => {
  it('starts with the intro line and the catalog when nothing is connected', () => {
    render(<McpSection {...props()} />);
    expect(screen.getByText('MCP servers')).toBeTruthy();
    expect(screen.getByText('Agents in new chats get the tools of these servers')).toBeTruthy();
    expect(screen.getByText('Issues, projects and cycles')).toBeTruthy();
    expect(screen.getByLabelText('Add your own server')).toBeTruthy();
  });

  it('opens the popup in the click, then sends it to the sign-in and waits for its word', async () => {
    const popup = fakePopup();
    const open = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
    const p = props();
    render(<McpSection {...p} />);
    fireEvent.click(screen.getAllByText('Connect')[0] as HTMLElement);
    // Before any answer of the machine: a window opened later would be blocked.
    expect(open).toHaveBeenCalledWith('about:blank', 'harnessly-mcp', 'popup,width=520,height=720');
    await waitFor(() => expect(p.onSignIn).toHaveBeenCalledWith('m1'));
    expect(vi.mocked(p.onAdd).mock.calls[0]?.[0]).toMatchObject({
      url: 'https://mcp.linear.app/mcp',
      catalogId: 'linear',
      header: null,
    });
    await waitFor(() => expect(popup.location.href).toBe(AUTH_URL));
    expect(p.onRefresh).not.toHaveBeenCalled();
    // Another origin's message is not the callback's.
    act(() => {
      window.dispatchEvent(
        new MessageEvent('message', {
          origin: 'https://evil.example',
          data: { type: 'harnessly-mcp', id: 'm1', ok: true },
        }),
      );
    });
    expect(p.onRefresh).not.toHaveBeenCalled();
    act(() => {
      window.dispatchEvent(
        new MessageEvent('message', {
          origin: window.location.origin,
          data: { type: 'harnessly-mcp', id: 'm1', ok: true },
        }),
      );
    });
    await waitFor(() => expect(p.onRefresh).toHaveBeenCalled());
  });

  it('leaves a link to the sign-in when the browser blocks the popup', async () => {
    vi.spyOn(window, 'open').mockReturnValue(null);
    const signin = readMcpServer(server({ state: 'signin', tools: [], me: '' }));
    render(<McpSection {...props({ mcp: { servers: [signin], catalog } })} />);
    expect(screen.getByText('Agents cannot use it until you sign in')).toBeTruthy();
    fireEvent.click(screen.getByText('Sign in'));
    const link = (await screen.findByText('Open sign-in')).closest('a');
    expect(link?.getAttribute('href')).toBe(AUTH_URL);
    expect(screen.getByText('The browser blocked the sign-in window')).toBeTruthy();
  });

  it('asks for a token right in the card of a server that takes one', async () => {
    const open = vi.spyOn(window, 'open');
    const p = props({
      onAdd: vi.fn(async () => ({
        server: readMcpServer(server({ id: 'g1', auth: 'header' })),
        authStart: null,
      })),
    });
    render(<McpSection {...p} />);
    fireEvent.click(screen.getAllByText('Connect')[1] as HTMLElement);
    expect((screen.getByLabelText('Header') as HTMLInputElement).value).toBe('Authorization');
    expect(screen.getByText('Create a token').closest('a')?.getAttribute('href')).toBe(github.tokenUrl);
    const field = screen.getByLabelText('Token') as HTMLInputElement;
    expect(field.type).toBe('password');
    fireEvent.change(field, { target: { value: TOKEN } });
    fireEvent.click(screen.getAllByText('Connect').at(-1) as HTMLElement);
    await waitFor(() => expect(p.onAdd).toHaveBeenCalled());
    expect(vi.mocked(p.onAdd).mock.calls[0]?.[0].header).toEqual({
      name: 'Authorization',
      value: `Bearer ${TOKEN}`,
    });
    expect(open).not.toHaveBeenCalled();
  });

  it('opens the token form of a kept server that asks for one, with no popup', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    const kept = readMcpServer(
      server({
        id: 'x1',
        name: 'Acme',
        catalogId: '',
        state: 'signin',
        tools: [],
        me: '',
        oauthUnavailable: true,
      }),
    );
    const p = props({
      onProbe: vi.fn(async () => ({
        probe: readMcpProbe({ url: 'https://mcp.acme.dev/mcp', auth: 'header', name: 'Acme' }),
      })),
      onAdd: vi.fn(async () => ({
        server: kept,
        authStart: readAuthStart({
          auth: 'header',
          headerName: 'X-API-Key',
          oauthUnavailable: true,
          tokenUrl: 'https://acme.dev/tokens',
        }),
      })),
    });
    const { rerender } = render(<McpSection {...p} />);
    fireEvent.change(screen.getByLabelText('Add your own server'), {
      target: { value: 'https://mcp.acme.dev/mcp' },
    });
    fireEvent.click(screen.getByText('Find'));
    fireEvent.change(await screen.findByLabelText('Token'), { target: { value: 'k1' } });
    // Catalog cards have their own "Connect"; the own-server form is the last one.
    fireEvent.click(screen.getAllByText('Connect').at(-1) as HTMLElement);
    await waitFor(() => expect(p.onAdd).toHaveBeenCalled());
    // The binding takes the kept server into the list.
    rerender(<McpSection {...p} mcp={{ servers: [kept], catalog }} />);
    expect(await screen.findByText(/needs a token here/)).toBeTruthy();
    // The paste card folds away; the row's form stays.
    await waitFor(() => expect(screen.getAllByLabelText('Header')).toHaveLength(1));
    expect((screen.getByLabelText('Header') as HTMLInputElement).value).toBe('X-API-Key');
    expect(screen.getAllByText('Create a token').at(-1)?.closest('a')?.getAttribute('href')).toBe(
      'https://acme.dev/tokens',
    );
    expect(open).not.toHaveBeenCalled();
  });

  it('lists a server by its name and whose it is, and says nothing of its state when fine', () => {
    render(<McpSection {...props({ mcp: { servers: [readMcpServer(server())], catalog } })} />);
    expect(screen.getByText('mcp.linear.app/mcp · as anna@acme.com')).toBeTruthy();
    expect(screen.queryByText(/sign in/i)).toBeNull();
    // Connected already: no longer in the catalog.
    expect(screen.queryByText('Issues, projects and cycles')).toBeNull();
    const toggle = screen.getByText('2 tools').closest('button') as HTMLButtonElement;
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByText('list_issues')).toBeTruthy();
  });

  it('mutes a server turned off for agents', () => {
    const off = readMcpServer(server({ enabled: false }));
    const { container } = render(<McpSection {...props({ mcp: { servers: [off], catalog } })} />);
    expect(container.querySelector('.hx-mcp-row[data-off]')).toBeTruthy();
    expect(screen.getByText(/off for agents/)).toBeTruthy();
  });

  it('says how a pasted server signs in', async () => {
    const p = props();
    render(<McpSection {...p} />);
    fireEvent.change(screen.getByLabelText('Add your own server'), {
      target: { value: 'https://mcp.acme.dev/mcp' },
    });
    fireEvent.click(screen.getByText('Find'));
    expect(await screen.findByText('Signs in on its own page')).toBeTruthy();
    expect(p.onProbe).toHaveBeenCalledWith('https://mcp.acme.dev/mcp');
  });

  it('talks to the runtime: list, connect, sign in', async () => {
    const popup = fakePopup();
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
    const calls: { url: string; method: string; body: string }[] = [];
    setFetch(async (url, init) => {
      const method = init.method ?? 'GET';
      calls.push({ url, method, body: String(init.body ?? '') });
      if (pathOf(url).endsWith('/mcp/m1/signin')) return json(200, { authUrl: AUTH_URL });
      if (method === 'POST' && pathOf(url).endsWith('/mcp'))
        return json(200, {
          server: server({ state: 'signin', tools: [], me: '' }),
          authStart: { auth: 'oauth' },
        });
      if (pathOf(url).endsWith('/mcp')) return json(200, { servers: [], catalog: [linear] });
      return json(200, { connections: [] });
    });
    render(<ConnectionsPage machine={MACHINE} />);
    fireEvent.click(await screen.findByText('Connect'));
    await waitFor(() => expect(popup.location.href).toBe(AUTH_URL));
    const add = calls.find((c) => c.method === 'POST' && pathOf(c.url).endsWith('/mcp'));
    expect(add?.url).toContain('/api/platform/mcp');
    expect(JSON.parse(add?.body ?? '{}')).toEqual({
      url: 'https://mcp.linear.app/mcp',
      catalogId: 'linear',
      transport: 'http',
    });
    const signin = calls.find((c) => pathOf(c.url).endsWith('/mcp/m1/signin'));
    expect(JSON.parse(signin?.body ?? '{}').origin).toBe(window.location.origin);
  });

  it('shows no MCP part on a machine whose runtime has none yet', async () => {
    setFetch(async (url) =>
      pathOf(url).endsWith('/mcp')
        ? new Response('404 page not found', { status: 404 })
        : json(200, { connections: [] }),
    );
    render(<ConnectionsPage machine={MACHINE} />);
    await screen.findByText(/A link is enough/);
    await waitFor(() => expect(screen.queryByText('MCP servers')).toBeNull());
  });
});
