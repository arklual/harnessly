// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { ConnectionsPage } from '@/next/bindings/settings-connections';
import { setFetch } from '@/next/core/net/http';
import { readConn, readDetection } from '@/next/features/connections/api';
import ConnectionsSection, {
  type IConnectionsSectionProps,
} from '@/next/features/connections/connections-section';
import { withPrimary } from '@/next/features/connections/model';
import { pinLocale } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

beforeAll(() => {
  pinLocale('en');
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
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
});
afterEach(() => {
  cleanup();
  setFetch((url, init) => fetch(url, init));
});

const TOKEN = 'ATAT' + 'x9'.repeat(20);
const MACHINE = 'local' as MachineId;

// A Detection as POST <platform>/connections/detect answers it (internal/connections/model.go).
const jiraDC = {
  kind: 'jira',
  baseUrl: 'https://jira.acme.com/jira',
  deployment: 'self-hosted',
  version: '9.12.4',
  edition: 'data-center',
  title: 'Acme Jira',
  methods: [
    {
      id: 'pat',
      fields: [{ id: 'token', secret: true, placeholder: 'NjM0…' }],
      tokenUrl: 'https://jira.acme.com/jira/secure/ViewProfile.jspa',
      recommended: true,
    },
    {
      id: 'basic',
      fields: [
        { id: 'username', secret: false },
        { id: 'password', secret: true },
      ],
    },
  ],
  capabilities: ['issues'],
  extra: { serverId: 'B1C2' },
};

// A Conn as the list answers it (internal/connections/provider.go).
const conn = (over: Record<string, unknown> = {}) => ({
  id: 'c1',
  kind: 'jira',
  name: 'Acme Jira',
  baseUrl: 'https://jira.acme.com/jira',
  deployment: 'self-hosted',
  version: '9.12.4',
  method: 'pat',
  capabilities: ['issues'],
  me: { name: 'Anna Petrova', login: 'anna' },
  state: 'ok',
  createdAt: '2026-11-03T10:00:00Z',
  ...over,
});

const props = (over: Partial<IConnectionsSectionProps> = {}): IConnectionsSectionProps => ({
  connections: [],
  onDetect: vi.fn(async () => ({ detection: readDetection(jiraDC) })),
  onConnect: vi.fn(async () => null),
  onCheck: vi.fn(async () => null),
  onPrimary: vi.fn(async () => null),
  onRename: vi.fn(async () => null),
  onRemove: vi.fn(async () => null),
  ...over,
});

// The machine's path may carry a query (which machine): requests are told apart by their path alone.
const pathOf = (url: string) => new URL(url, 'http://page.test').pathname;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const paste = (link: string) => {
  fireEvent.change(screen.getByLabelText('Paste a link to your tracker, wiki or code host'), {
    target: { value: link },
  });
  fireEvent.click(screen.getByText('Find'));
};

describe('the connections readers', () => {
  it('keep a detection to send back and read a connection without its secrets', () => {
    const d = readDetection(jiraDC);
    expect(d.methods[0]?.recommended).toBe(true);
    expect(d.methods[1]?.fields[0]?.secret).toBe(false);
    expect(d.wire.extra).toEqual({ serverId: 'B1C2' });
    expect(readConn(conn()).me?.name).toBe('Anna Petrova');
    expect(() => readConn(conn({ deployment: 'moon' }))).toThrow();
  });

  it('makes one primary per capability, as the runtime does', () => {
    const a = readConn(conn({ id: 'a', primary: true }));
    const b = readConn(conn({ id: 'b' }));
    const wiki = readConn(conn({ id: 'w', kind: 'confluence', capabilities: ['pages'], primary: true }));
    const next = withPrimary([a, b, wiki], { ...b, primary: true });
    expect(next.map((c) => c.primary)).toEqual([false, true, true]);
  });
});

describe('Settings → Connections', { timeout: 20_000 }, () => {
  it('starts with one line and the paste field', () => {
    render(<ConnectionsSection {...props()} />);
    expect(screen.getByText(/A link is enough/)).toBeTruthy();
    expect(screen.getByLabelText('Paste a link to your tracker, wiki or code host')).toBeTruthy();
  });

  it('shows what a link is and asks for its token, then connects', async () => {
    const p = props();
    render(<ConnectionsSection {...p} />);
    paste('https://jira.acme.com/jira/browse/ABC-12');
    await screen.findByText('Self-hosted · version 9.12.4');
    expect(p.onDetect).toHaveBeenCalledWith('https://jira.acme.com/jira/browse/ABC-12');
    expect(screen.getByText('jira.acme.com/jira')).toBeTruthy();
    // The recommended method is preselected; the other is one press away.
    expect(screen.getByRole('radio', { name: 'Personal token' }).getAttribute('aria-checked')).toBe('true');
    const link = screen.getByText('Create a token').closest('a');
    expect(link?.getAttribute('target')).toBe('_blank');
    const field = screen.getByLabelText('Token') as HTMLInputElement;
    expect(field.type).toBe('password');
    fireEvent.change(field, { target: { value: TOKEN } });
    fireEvent.click(screen.getByText('Connect'));
    await waitFor(() => expect(p.onConnect).toHaveBeenCalled());
    const input = vi.mocked(p.onConnect).mock.calls[0]?.[0];
    expect(input?.method).toBe('pat');
    expect(input?.credentials).toEqual({ token: TOKEN });
  });

  it('switches to login and password', async () => {
    render(<ConnectionsSection {...props()} />);
    paste('jira.acme.com/jira');
    fireEvent.click(await screen.findByRole('radio', { name: 'Login and password' }));
    expect(screen.getByLabelText('Login')).toBeTruthy();
    expect((screen.getByLabelText('Password') as HTMLInputElement).type).toBe('password');
  });

  it('says why a token was refused and forgets it', async () => {
    const p = props({
      onConnect: vi.fn(
        async () => 'The service refused these credentials — check the token and its permissions',
      ),
    });
    render(<ConnectionsSection {...p} />);
    paste('jira.acme.com');
    fireEvent.change(await screen.findByLabelText('Token'), { target: { value: TOKEN } });
    fireEvent.click(screen.getByText('Connect'));
    expect(await screen.findByText(/refused these credentials/)).toBeTruthy();
    expect((screen.getByLabelText('Token') as HTMLInputElement).value).toBe('');
  });

  it('lists a connection by its name, says whose it is, and nothing about its state when fine', () => {
    render(<ConnectionsSection {...props({ connections: [readConn(conn())] })} />);
    expect(screen.getByText('Acme Jira')).toBeTruthy();
    expect(screen.getByText('Jira · jira.acme.com/jira · as Anna Petrova')).toBeTruthy();
    expect(screen.queryByText('Primary')).toBeNull();
    expect(screen.queryByText(/did not answer/)).toBeNull();
  });

  it('marks the primary only when two do the same work, and says what is wrong', () => {
    const list = [
      readConn(conn({ id: 'a', primary: true })),
      readConn(conn({ id: 'b', name: 'Old Jira', state: 'denied' })),
    ];
    render(<ConnectionsSection {...props({ connections: list })} />);
    expect(screen.getAllByText('Primary')).toHaveLength(1);
    expect(screen.getByText(/no longer accepts this token/)).toBeTruthy();
  });

  it('says a connection of the machine is set by it', () => {
    render(
      <ConnectionsSection {...props({ connections: [readConn(conn({ env: true, kind: 'youtrack' }))] })} />,
    );
    expect(screen.getByText('Set by the machine')).toBeTruthy();
  });

  it('talks to the runtime: list, detect, connect', async () => {
    const calls: { url: string; method: string; body: string }[] = [];
    setFetch(async (url, init) => {
      const method = init.method ?? 'GET';
      // An older runtime without MCP: the MCP part stays hidden.
      if (pathOf(url).endsWith('/mcp')) return new Response('', { status: 404 });
      calls.push({ url: pathOf(url), method, body: String(init.body ?? '') });
      if (pathOf(url).endsWith('/connections/detect')) return json(200, { detection: jiraDC });
      if (method === 'POST' && pathOf(url).endsWith('/connections')) return json(200, { connection: conn() });
      return json(200, { connections: [], kinds: ['jira'] });
    });
    render(<ConnectionsPage machine={MACHINE} />);
    await screen.findByText(/A link is enough/);
    paste('https://jira.acme.com/jira/browse/ABC-12');
    fireEvent.change(await screen.findByLabelText('Token'), { target: { value: TOKEN } });
    fireEvent.click(screen.getByText('Connect'));
    expect(await screen.findByText('Jira · jira.acme.com/jira · as Anna Petrova')).toBeTruthy();
    expect(calls[0]?.url).toContain('/api/platform/connections');
    const detect = calls.find((c) => c.url.endsWith('/connections/detect'));
    expect(JSON.parse(detect?.body ?? '{}')).toEqual({ url: 'https://jira.acme.com/jira/browse/ABC-12' });
    const connect = calls.find((c) => c.method === 'POST' && c.url.endsWith('/connections'));
    const sent = JSON.parse(connect?.body ?? '{}');
    expect(sent.method).toBe('pat');
    expect(sent.credentials).toEqual({ token: TOKEN });
    expect(sent.detection.extra).toEqual({ serverId: 'B1C2' });
  });

  it('reads a refused link as its words', async () => {
    setFetch(async (url) => {
      if (pathOf(url).endsWith('/mcp')) return new Response('', { status: 404 });
      if (pathOf(url).endsWith('/connections/detect')) return json(422, { error: 'unknown-product' });
      return json(200, { connections: [], kinds: [] });
    });
    render(<ConnectionsPage machine={MACHINE} />);
    await screen.findByText(/A link is enough/);
    paste('https://example.com');
    expect(await screen.findByText(/could not recognize this link/)).toBeTruthy();
  });
});
