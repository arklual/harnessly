// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { ConnectionsPage } from '@/next/bindings/settings-connections';
import { setFetch } from '@/next/core/net/http';
import {
  readAuthStart,
  readMcpCatalogEntry,
  readMcpProbe,
  readMcpServer,
} from '@/next/features/connections/mcp-api';
import { catalogGroups, headerValue, openCatalog } from '@/next/features/connections/mcp-model';
import McpSection, { type IMcpSectionProps } from '@/next/features/connections/mcp-section';
import { canFromTools, findService, linksOf, stepsOf } from '@/next/features/connections/service-model';
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
  // A catalog card scrolls the form into view; jsdom draws nothing to scroll.
  Element.prototype.scrollIntoView = () => {};
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
  kind: 'tasks',
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
  kind: 'code',
  url: 'https://api.githubcopilot.com/mcp/',
  transport: 'http',
  auth: 'header',
  headerName: 'Authorization',
  tokenUrl: 'https://github.com/settings/personal-access-tokens/new',
  docsUrl: 'https://github.com/github/github-mcp-server',
  blurb: 'Repositories, issues and pull requests',
  blurbRu: 'Репозитории, задачи и пул-реквесты',
};

// A local server and an API from the catalog: keys only, nothing to probe.
const exa = {
  id: 'exa',
  name: 'Exa Search',
  kind: 'search',
  url: '',
  transport: 'stdio',
  runtime: 'npx',
  auth: 'env',
  command: ['npx', '-y', 'exa-mcp-server'],
  env: [
    { name: 'EXA_API_KEY', label: 'Exa API key', secret: true, hint: 'From dashboard.exa.ai' },
    { name: 'EXA_REGION', label: 'Region', optional: true, default: 'us' },
  ],
  tokenUrl: 'https://dashboard.exa.ai/api-keys',
  blurb: 'Web search for agents',
  blurbRu: 'Поиск по вебу для агентов',
};
const open = {
  id: 'open-meteo',
  name: 'Open-Meteo',
  kind: 'utility',
  url: 'https://api.open-meteo.com',
  transport: 'api',
  auth: 'none',
  blurb: 'Weather forecasts',
  blurbRu: 'Прогноз погоды',
};

const catalog = [readMcpCatalogEntry(linear), readMcpCatalogEntry(github)];
const keyedCatalog = [...catalog, readMcpCatalogEntry(exa), readMcpCatalogEntry(open)];

const props = (over: Partial<IMcpSectionProps> = {}): IMcpSectionProps => ({
  mcp: { servers: [], catalog },
  onProbe: vi.fn(async (url: string) => ({
    probe: readMcpProbe({ url, transport: 'http', auth: 'oauth', name: 'Acme' }),
  })),
  onAdd: vi.fn(async () => ({
    server: readMcpServer(server({ state: 'signin', tools: [], me: '' })),
    authStart: readAuthStart({ auth: 'oauth' }),
  })),
  onSignIn: vi.fn(async () => ({ authUrl: AUTH_URL })),
  onRefresh: vi.fn(async () => undefined),
  onHeader: vi.fn(async () => null),
  onEnv: vi.fn(async () => null),
  onCheck: vi.fn(async () => null),
  onRename: vi.fn(async () => null),
  onEnable: vi.fn(async () => null),
  onRemove: vi.fn(async () => null),
  ...over,
});

/** A popup as window.open gives it: blank at first, sent to the sign-in later. */
const fakePopup = () => ({ closed: false, close: vi.fn(), location: { href: 'about:blank' } });

/** A row of the list: a button named by its service. */
const card = (name: string) => screen.getByRole('button', { name: new RegExp(`^${name}`) });

/** The address of a server of one's own is folded like a group; opening it gives the field. */
const ownField = () => {
  fireEvent.click(screen.getByRole('button', { name: /^Add by hand/ }));
  return screen.getByLabelText('MCP server address');
};

/** The page of the open service: the list behind it stays mounted, hidden, and has the same words. */
const page = () => within(screen.getByRole('article'));

/** A group of the catalog is folded to its name and count until it is pressed. */
const openGroup = (name: string) =>
  fireEvent.click(screen.getByRole('button', { name: new RegExp(`^${name}`) }));

/**
 * The section as the binding runs it: the address (`item`) is the binding's, and so is the list, which takes in the
 * server a connect kept.
 */
const Host = (p: IMcpSectionProps) => {
  const [item, setItem] = useState<string | null>(p.item ?? null);
  const [mcp, setMcp] = useState(p.mcp);
  return (
    <McpSection
      {...p}
      mcp={mcp}
      item={item}
      onOpen={setItem}
      onAdd={async (input) => {
        const got = await p.onAdd(input);
        if (!('error' in got) && mcp !== null) setMcp({ ...mcp, servers: [...mcp.servers, got.server] });
        return got;
      }}
    />
  );
};

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

  it('reads local servers and APIs, tolerant of what a runtime leaves out', () => {
    const entry = readMcpCatalogEntry(exa);
    expect(entry.transport).toBe('stdio');
    expect(entry.url).toBe('');
    expect(entry.auth).toBe('env');
    expect(entry.env.map((f) => [f.name, f.secret, f.optional, f.default])).toEqual([
      ['EXA_API_KEY', true, false, ''],
      ['EXA_REGION', false, true, 'us'],
    ]);
    expect(readMcpCatalogEntry(open).env).toEqual([]);
    expect(readMcpCatalogEntry({ ...linear, kind: 'finance' }).kind).toBe('finance');
    const s = readMcpServer(
      server({ transport: 'stdio', auth: 'env', env: exa.env, problem: 'uvx is not installed' }),
    );
    expect(s.env).toHaveLength(2);
    expect(s.problem).toBe('uvx is not installed');
    expect(readMcpServer(server()).env).toEqual([]);
    expect(readMcpServer(server()).problem).toBe('');
  });

  it("puts «Bearer» (or the entry's own prefix) before a bare token in Authorization only", () => {
    expect(headerValue('Authorization', ` ${TOKEN} `, '')).toBe(`Bearer ${TOKEN}`);
    expect(headerValue('Authorization', TOKEN, 'OAuth ')).toBe(`OAuth ${TOKEN}`);
    expect(headerValue('authorization', `Token ${TOKEN}`, 'OAuth ')).toBe(`Token ${TOKEN}`);
    expect(headerValue('X-API-Key', TOKEN, '')).toBe(TOKEN);
  });

  it('groups the catalog by kind and finds by name or what it gives', () => {
    expect(() => readMcpCatalogEntry({ ...linear, kind: 'magic' })).toThrow();
    const all = catalogGroups(catalog, (entry) => entry, '');
    expect(all.map((group) => group.kind)).toEqual(['tasks', 'code']);
    expect(catalogGroups(catalog, (entry) => entry, 'git').flatMap((g) => g.items.map((e) => e.id))).toEqual([
      'github',
    ]);
    expect(catalogGroups(catalog, (entry) => entry, 'nothing-like-it')).toEqual([]);
  });
});

describe('Settings → Connections → MCP servers', { timeout: 20_000 }, () => {
  it('starts with the intro line, the groups of the catalog folded, and a field for a server of one’s own', () => {
    render(<Host {...props()} />);
    expect(screen.getByText('MCP servers')).toBeTruthy();
    expect(screen.getByText('Agents in new chats get the tools of these servers')).toBeTruthy();
    expect(screen.getByRole('button', { name: /^Tasks/ }).getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByText('Linear')).toBeNull();
    expect(ownField()).toBeTruthy();
  });

  it('opens a group on a press, and the search opens the groups that have what it finds', () => {
    render(<Host {...props()} />);
    openGroup('Tasks');
    expect(screen.getByText('Issues, projects and cycles')).toBeTruthy();
    expect(screen.queryByText('GitHub')).toBeNull();
    fireEvent.change(screen.getByLabelText('Search services'), { target: { value: 'pull requests' } });
    expect(screen.getByText('GitHub')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Tasks/ })).toBeNull();
    fireEvent.change(screen.getByLabelText('Search services'), { target: { value: 'zzz' } });
    expect(screen.getByText('Nothing found')).toBeTruthy();
  });

  it('a service of the catalog opens its page: nothing is kept, no window opens, «back» finds the list', async () => {
    const open = vi.spyOn(window, 'open');
    const p = props();
    render(<Host {...p} />);
    openGroup('Tasks');
    fireEvent.click(card('Linear'));
    expect(await screen.findByRole('heading', { name: 'Linear' })).toBeTruthy();
    expect(screen.getByText('How to connect')).toBeTruthy();
    expect(screen.getByText('Sign in with Linear', { selector: 'button *' })).toBeTruthy();
    expect(screen.getByText('Documentation').closest('a')?.getAttribute('href')).toBe(linear.docsUrl);
    expect(p.onProbe).not.toHaveBeenCalled();
    expect(p.onAdd).not.toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('All connections'));
    expect(screen.queryByRole('heading', { name: 'Linear' })).toBeNull();
    // The list is as it was left: the group is still open.
    expect(screen.getByText('Issues, projects and cycles')).toBeTruthy();
  });

  it('says so for an address that is no service', () => {
    render(<Host {...props({ item: 'nope' })} />);
    expect(screen.getByText(/no such service/)).toBeTruthy();
    fireEvent.click(screen.getByText('All connections'));
    expect(screen.getByText('MCP servers')).toBeTruthy();
  });

  it('opens the popup in the click, then sends it to the sign-in and waits for its word', async () => {
    const popup = fakePopup();
    const open = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
    const p = props({ item: 'linear' });
    render(<Host {...p} />);
    fireEvent.click(await screen.findByText('Sign in with Linear'));
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
    render(<Host {...props({ item: 'linear', mcp: { servers: [signin], catalog } })} />);
    expect(screen.getByText('Sign-in not finished')).toBeTruthy();
    fireEvent.click(screen.getByText('Sign in'));
    const link = (await screen.findByText('Open sign-in')).closest('a');
    expect(link?.getAttribute('href')).toBe(AUTH_URL);
    expect(screen.getByText('The browser blocked the sign-in window')).toBeTruthy();
  });

  it('asks for a token on the page of a service that takes one, and sends it as Bearer', async () => {
    const open = vi.spyOn(window, 'open');
    const p = props({
      item: 'github',
      onAdd: vi.fn(async () => ({
        server: readMcpServer(server({ id: 'g1', name: 'GitHub', catalogId: 'github', auth: 'header' })),
        authStart: null,
      })),
    });
    render(<Host {...p} />);
    expect(screen.getByText('Create a token').closest('a')?.getAttribute('href')).toBe(github.tokenUrl);
    const field = screen.getByLabelText('Token') as HTMLInputElement;
    expect(field.type).toBe('password');
    const connect = screen.getByText('Connect').closest('button') as HTMLButtonElement;
    expect(connect.disabled).toBe(true);
    fireEvent.change(field, { target: { value: TOKEN } });
    fireEvent.click(connect);
    await waitFor(() => expect(p.onAdd).toHaveBeenCalled());
    expect(vi.mocked(p.onAdd).mock.calls[0]?.[0].header).toEqual({
      name: 'Authorization',
      value: `Bearer ${TOKEN}`,
    });
    expect(open).not.toHaveBeenCalled();
    // Kept: the page turns into the state of the server.
    await waitFor(() => expect(page().getByText('Online')).toBeTruthy());
    expect(screen.queryByText('How to connect')).toBeNull();
  });

  it('opens the token form on the page of a kept server that asks for one, with no popup', async () => {
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
    render(<Host {...p} />);
    fireEvent.change(ownField(), {
      target: { value: 'https://mcp.acme.dev/mcp' },
    });
    fireEvent.click(screen.getByText('Find'));
    fireEvent.change(await screen.findByLabelText('Token'), { target: { value: 'k1' } });
    fireEvent.click(screen.getByText('Connect'));
    await waitFor(() => expect(p.onAdd).toHaveBeenCalled());
    // The kept server is in the list; its page has the token form open.
    fireEvent.click(await screen.findByRole('button', { name: /^Acme/ }));
    expect((await screen.findByLabelText('Header')) as HTMLInputElement).toBeTruthy();
    expect((screen.getByLabelText('Header') as HTMLInputElement).value).toBe('X-API-Key');
    expect(screen.getByText('Create a token').closest('a')?.getAttribute('href')).toBe(
      'https://acme.dev/tokens',
    );
    expect(open).not.toHaveBeenCalled();
  });

  it('lists a connected server by its name, whose it is and how many tools, and it leaves the catalog', () => {
    render(<Host {...props({ mcp: { servers: [readMcpServer(server())], catalog } })} />);
    expect(screen.getByText('mcp.linear.app/mcp · as anna@acme.com · 2 tools')).toBeTruthy();
    expect(screen.getByText('Online')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Tasks/ })).toBeNull();
  });

  it('a connected server’s page: its state, what its tools let the agent do, the tools folded', () => {
    const many = Array.from({ length: 8 }, (_, n) => [
      { name: `list_page${n}`, description: 'List things' },
      { name: `create_page${n}`, description: 'Create a thing' },
    ]).flat();
    const rich = readMcpServer(
      server({ tools: [...many, { name: 'search_pages', description: 'Search.' }] }),
    );
    const { container } = render(<Host {...props({ item: 'linear', mcp: { servers: [rich], catalog } })} />);
    expect(screen.getByRole('heading', { name: 'Linear' })).toBeTruthy();
    expect(page().getByText('Online')).toBeTruthy();
    expect(screen.getByText('What the agent will be able to do')).toBeTruthy();
    expect(screen.getByText(/From the server's real tools: 17 in all/)).toBeTruthy();
    expect((container.querySelector('details.hx-svc-tools') as HTMLDetailsElement).open).toBe(false);
    for (const name of ['Check', 'Turn off', 'Rename', 'Disconnect'])
      expect(page().getAllByText(name).length).toBeGreaterThan(0);
  });

  it('its buttons talk to the machine: check, turn off, and disconnect after «sure?»', async () => {
    const p = props({ item: 'linear', mcp: { servers: [readMcpServer(server())], catalog } });
    render(<Host {...p} />);
    fireEvent.click(screen.getByText('Check'));
    await waitFor(() => expect(p.onCheck).toHaveBeenCalledWith('m1'));
    expect(await screen.findByText('Works')).toBeTruthy();
    fireEvent.click(screen.getByText('Turn off'));
    await waitFor(() => expect(p.onEnable).toHaveBeenCalledWith('m1', false));
    fireEvent.click(screen.getAllByText('Disconnect')[0]);
    expect(screen.getByText(/Disconnect «Linear»/)).toBeTruthy();
    expect(p.onRemove).not.toHaveBeenCalled();
    fireEvent.click(screen.getAllByText('Disconnect').at(-1) as HTMLElement);
    await waitFor(() => expect(p.onRemove).toHaveBeenCalledWith('m1'));
  });

  it('mutes a server turned off for agents', () => {
    const off = readMcpServer(server({ enabled: false }));
    const { container } = render(<Host {...props({ mcp: { servers: [off], catalog } })} />);
    expect(container.querySelector('.hx-cn-row[data-off]')).toBeTruthy();
    expect(screen.getByText(/off for agents/)).toBeTruthy();
    expect(screen.getByText('Off')).toBeTruthy();
  });

  it('says how a pasted server signs in', async () => {
    const p = props();
    render(<Host {...p} />);
    fireEvent.change(ownField(), {
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
    render(<ConnectionsPage machine={MACHINE} item="linear" />);
    fireEvent.click(await screen.findByText('Sign in with Linear'));
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

  it('a local server asks for its keys on its page: no probe, and «Connect» keeps it with the keys', async () => {
    const p = props({
      item: 'exa',
      mcp: { servers: [], catalog: keyedCatalog },
      onAdd: vi.fn(async () => ({
        server: readMcpServer(
          server({
            id: 'e1',
            name: 'Exa Search',
            catalogId: 'exa',
            url: 'stdio:exa',
            transport: 'stdio',
            auth: 'env',
          }),
        ),
        authStart: null,
      })),
    });
    render(<Host {...p} />);
    expect(screen.getByText(/starts on this machine through npx\/uvx/)).toBeTruthy();
    expect(p.onProbe).not.toHaveBeenCalled();
    expect(screen.getByText('From dashboard.exa.ai')).toBeTruthy();
    expect(screen.getByText('Where to get the key').closest('a')?.getAttribute('href')).toBe(exa.tokenUrl);
    const key = screen.getByLabelText('Exa API key') as HTMLInputElement;
    expect(key.type).toBe('password');
    // The optional field starts with its default and does not hold «Connect» back.
    expect((screen.getByLabelText('Region (optional)') as HTMLInputElement).value).toBe('us');
    const connect = screen.getByText('Connect').closest('button') as HTMLButtonElement;
    expect(connect.disabled).toBe(true);
    fireEvent.change(key, { target: { value: ' exa-secret ' } });
    expect(connect.disabled).toBe(false);
    fireEvent.click(connect);
    await waitFor(() => expect(p.onAdd).toHaveBeenCalled());
    expect(vi.mocked(p.onAdd).mock.calls[0]?.[0]).toMatchObject({
      url: '',
      catalogId: 'exa',
      header: null,
      env: { EXA_API_KEY: 'exa-secret', EXA_REGION: 'us' },
    });
    // Kept: the page turns into the state of the server.
    await waitFor(() => expect(screen.queryByLabelText('Exa API key')).toBeNull());
  });

  it('says the server is starting while a local server is added, and shows a refusal', async () => {
    let refuse: (answer: { error: string }) => void = () => {};
    const p = props({
      item: 'exa',
      mcp: { servers: [], catalog: keyedCatalog },
      onAdd: vi.fn(() => new Promise<{ error: string }>((resolve) => (refuse = resolve))),
    });
    render(<Host {...p} />);
    fireEvent.change(screen.getByLabelText('Exa API key'), { target: { value: 'bad' } });
    fireEvent.click(screen.getByText('Connect'));
    expect(await screen.findByText(/first time can take a couple of minutes/)).toBeTruthy();
    act(() => refuse({ error: 'The server refused' }));
    expect(await screen.findByText('The server refused')).toBeTruthy();
    expect(screen.queryByText(/first time can take/)).toBeNull();
  });

  it('connects an API without keys in one press', async () => {
    const p = props({
      item: 'open-meteo',
      mcp: { servers: [], catalog: keyedCatalog },
      onAdd: vi.fn(async () => ({
        server: readMcpServer(
          server({
            id: 'o1',
            name: 'Open-Meteo',
            catalogId: 'open-meteo',
            url: 'https://api.open-meteo.com',
            transport: 'api',
            auth: 'none',
          }),
        ),
        authStart: null,
      })),
    });
    render(<Host {...p} />);
    expect(screen.getByText(/agents reach it through the hub/)).toBeTruthy();
    fireEvent.click(screen.getByText('Connect'));
    await waitFor(() => expect(p.onAdd).toHaveBeenCalled());
    expect(vi.mocked(p.onAdd).mock.calls[0]?.[0]).toMatchObject({ catalogId: 'open-meteo', env: {} });
    expect(p.onProbe).not.toHaveBeenCalled();
  });

  const localServer = (over: Record<string, unknown> = {}) =>
    server({
      id: 'e1',
      name: 'Exa Search',
      catalogId: 'exa',
      url: 'stdio:exa',
      transport: 'stdio',
      auth: 'env',
      env: exa.env,
      ...over,
    });

  it("a local server's page has a keys panel: an empty field keeps the key, and the problem shows", async () => {
    const local = readMcpServer(
      localServer({
        state: 'unreachable',
        tools: [],
        me: '',
        problem: 'uvx is not installed on this machine',
      }),
    );
    const p = props({ item: 'exa', mcp: { servers: [local], catalog: keyedCatalog } });
    render(<Host {...p} />);
    expect(page().getByText('Local server on this machine')).toBeTruthy();
    expect(screen.getByText('uvx is not installed on this machine')).toBeTruthy();
    expect(page().getByText('Not answering')).toBeTruthy();
    fireEvent.click(screen.getAllByText('Keys')[0]);
    const key = (await screen.findByLabelText('Exa API key')) as HTMLInputElement;
    expect(key.value).toBe('');
    const save = key
      .closest('.hx-svc-manage__panel')
      ?.querySelector('button.hx-set__btn') as HTMLButtonElement;
    expect(save.textContent).toBe('Save');
    expect(save.disabled).toBe(true);
    fireEvent.change(key, { target: { value: 'new-key' } });
    fireEvent.click(save);
    await waitFor(() => expect(p.onEnv).toHaveBeenCalledWith('e1', { EXA_API_KEY: 'new-key' }));
    expect(p.onHeader).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByLabelText('Exa API key')).toBeNull());
  });

  it('the search filters the catalog live, over names, words and group names', () => {
    const many = [
      ...keyedCatalog,
      ...['a', 'b', 'c', 'd'].map((n) =>
        readMcpCatalogEntry({
          ...linear,
          id: `t-${n}`,
          name: `Tasks ${n}`,
          url: `https://${n}.example.com/mcp`,
        }),
      ),
    ];
    render(<Host {...props({ mcp: { servers: [], catalog: many } })} />);
    // Searching opens the groups that have what it finds.
    fireEvent.change(screen.getByLabelText('Search services'), { target: { value: 'web search' } });
    expect(screen.getByText('Exa Search')).toBeTruthy();
    expect(screen.getByRole('button', { name: /^Search and web/ })).toBeTruthy();
    expect(screen.queryByText('Tasks a')).toBeNull();
    fireEvent.change(screen.getByLabelText('Search services'), { target: { value: 'utilities' } });
    expect(screen.getByText('Open-Meteo')).toBeTruthy();
    expect(screen.queryByText('Exa Search')).toBeNull();
  });

  it('talks to the runtime for keys: add with env, then PUT the new ones', async () => {
    const calls: { url: string; method: string; body: string }[] = [];
    const local = localServer();
    setFetch(async (url, init) => {
      const method = init.method ?? 'GET';
      calls.push({ url, method, body: String(init.body ?? '') });
      if (method === 'PUT' && pathOf(url).endsWith('/mcp/e1/env')) return json(200, { server: local });
      if (method === 'POST' && pathOf(url).endsWith('/mcp'))
        return json(200, { server: local, authStart: null });
      if (pathOf(url).endsWith('/mcp')) return json(200, { servers: [], catalog: [exa] });
      return json(200, { connections: [] });
    });
    render(<ConnectionsPage machine={MACHINE} item="exa" />);
    fireEvent.change(await screen.findByLabelText('Exa API key'), { target: { value: 'k1' } });
    fireEvent.click(screen.getByText('Connect'));
    await waitFor(() => expect(page().getByText(/^Local server on this machine/)).toBeTruthy());
    const add = calls.find((c) => c.method === 'POST' && pathOf(c.url).endsWith('/mcp'));
    expect(JSON.parse(add?.body ?? '{}')).toEqual({
      catalogId: 'exa',
      env: { EXA_API_KEY: 'k1', EXA_REGION: 'us' },
    });
    fireEvent.click(screen.getAllByText('Keys')[0]);
    const key = (await screen.findByLabelText('Exa API key')) as HTMLInputElement;
    fireEvent.change(key, { target: { value: 'k2' } });
    fireEvent.click(
      key.closest('.hx-svc-manage__panel')?.querySelector('button.hx-set__btn') as HTMLButtonElement,
    );
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT');
    expect(JSON.parse(put?.body ?? '{}')).toEqual({ env: { EXA_API_KEY: 'k2' } });
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

describe('the page of a service, from the catalog', () => {
  const guided = readMcpCatalogEntry({
    ...linear,
    about: 'Linear is a tracker.',
    can: ['Find issues', 'Create an issue', 'Change status'],
    canRu: ['Найдёт задачи', 'Заведёт задачу', 'Сменит статус'],
    steps: ['Do this', 'Do that'],
    links: [
      { kind: 'docs', url: linear.docsUrl },
      { kind: 'pricing', url: 'https://linear.app/pricing' },
    ],
  });

  it('finds the service of an address: a catalog id, a server id, a custom server, or nothing', () => {
    const kept = readMcpServer(server());
    const own = readMcpServer(server({ id: 'z9', name: 'Own', catalogId: '' }));
    expect(findService('linear', [kept], catalog)?.server?.id).toBe('m1');
    expect(findService('m1', [kept], catalog)?.entry?.id).toBe('linear');
    expect(findService('linear', [], catalog)?.server).toBeNull();
    expect(findService('z9', [own], catalog)).toEqual({ entry: null, server: own });
    expect(findService('nope', [kept], catalog)).toBeNull();
  });

  it("takes the entry's own steps and links, and the sign-in's generic steps when it has none", () => {
    expect(stepsOf(guided)).toEqual(['Do this', 'Do that']);
    expect(stepsOf(readMcpCatalogEntry(linear))).toHaveLength(3);
    expect(stepsOf(readMcpCatalogEntry(github))[0]).toMatch(/Create token/);
    const kinds = linksOf(guided).map((link) => link.kind);
    // Docs once, though the entry's docsUrl says it too; the order is the page's.
    expect(kinds).toEqual(['docs', 'pricing']);
  });

  it('reads what a connected server can do from its tools: things and what is done to them', () => {
    const tools = ['issue', 'project', 'cycle', 'comment'].flatMap((thing) =>
      ['list', 'get', 'create'].map((verb) => ({ name: `${verb}_${thing}`, description: '' })),
    );
    const lines = canFromTools(tools);
    expect(lines).toHaveLength(4);
    expect(lines[0]).toMatch(/^(issue|project|cycle|comment): list, get, create$/);
    // Names that say little: the first sentences of the descriptions.
    const vague = ['a', 'b', 'c', 'd', 'e', 'f'].map((n) => ({
      name: `tool${n}`,
      description: `Does ${n}. And more.`,
    }));
    expect(canFromTools(vague).slice(0, 2)).toEqual(['Does a.', 'Does b.']);
  });

  it('shows the guide of a service: its description, what the agent can do, steps and links', () => {
    render(<Host {...props({ item: 'linear', mcp: { servers: [], catalog: [guided] } })} />);
    expect(screen.getByText('Linear is a tracker.')).toBeTruthy();
    expect(screen.getByText('Create an issue')).toBeTruthy();
    expect(screen.getByText('Do that')).toBeTruthy();
    expect(screen.getByText('Prices').closest('a')?.getAttribute('href')).toBe('https://linear.app/pricing');
    expect(within(screen.getByRole('article')).getAllByRole('link')).toHaveLength(2);
  });
});
