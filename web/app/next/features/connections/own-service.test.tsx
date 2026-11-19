// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { readMcpCatalogEntry, readMcpServer } from '@/next/features/connections/mcp-api';
import McpSection, { type IMcpSectionProps } from '@/next/features/connections/mcp-section';
import { apiHeaderTemplate, commandWords } from '@/next/features/connections/own-service';
import { findService } from '@/next/features/connections/service-model';
import { pinLocale } from '@/next/i18n';
import { NEW_CHAT, draftText } from '@/next/stores/drafts';

beforeAll(() => {
  pinLocale('en');
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
  Element.prototype.scrollIntoView = () => {};
});
afterEach(cleanup);

const TYPED_KEY = 'sk-typed-' + '0123456789';

// A service of the person's own as GET <platform>/mcp answers it.
const own = (over: Record<string, unknown> = {}) =>
  readMcpServer({
    id: 'm9',
    name: 'Acme orders',
    slug: 'acme-orders',
    url: 'https://api.acme.example/v2',
    transport: 'api',
    auth: 'env',
    catalogId: 'custom-acme-orders',
    enabled: true,
    state: 'ok',
    tools: [{ name: 'api_request', description: 'An HTTP request' }],
    env: [{ name: 'API_KEY', label: 'API key', secret: true }],
    custom: {
      category: 'crm',
      letter: 'A',
      about: 'The internal orders API. Orders, customers, refunds.',
      aboutRu: '',
      can: ['Find an order by number'],
      steps: ['Ask the platform team for a key'],
      links: [{ kind: 'docs', url: 'https://wiki.acme.example/orders-api' }],
    },
    ...over,
  });

const props = (over: Partial<IMcpSectionProps> = {}): IMcpSectionProps => ({
  mcp: { servers: [own()], catalog: [] },
  onProbe: vi.fn(),
  onAdd: vi.fn(),
  onAddOwn: vi.fn(async () => ({ server: own({ id: 'm10', catalogId: 'custom-new' }), authStart: null })),
  onSignIn: vi.fn(),
  onRefresh: vi.fn(async () => undefined),
  onHeader: vi.fn(async () => null),
  onEnv: vi.fn(async () => null),
  onCheck: vi.fn(async () => null),
  onRename: vi.fn(async () => null),
  onEnable: vi.fn(async () => null),
  onRemove: vi.fn(async () => null),
  onDescribe: vi.fn(async () => null),
  ...over,
});

describe("a service of one's own", () => {
  it('has a page made of its record, found by its stable id and by its machine id', () => {
    const servers = [own()];
    for (const item of ['custom-acme-orders', 'm9']) {
      const service = findService(item, servers, [
        readMcpCatalogEntry({
          id: 'linear',
          name: 'Linear',
          kind: 'tasks',
          transport: 'http',
          auth: 'oauth',
        }),
      ]);
      expect(service?.entry?.about).toContain('internal orders API');
      expect(service?.entry?.kind).toBe('crm');
      expect(service?.server?.id).toBe('m9');
    }
  });

  it('shows its description on the page, and edits it by hand', async () => {
    const p = props({ item: 'custom-acme-orders' });
    render(<McpSection {...p} />);
    const page = document.querySelector('.hx-svc') as HTMLElement;
    expect(page.textContent).toContain('The internal orders API.');
    expect(page.textContent).toContain('Find an order by number');
    fireEvent.click(screen.getByRole('button', { name: 'Edit the description' }));
    fireEvent.change(screen.getByLabelText('What it is'), {
      target: { value: 'Orders and refunds of the shop.' },
    });
    fireEvent.change(screen.getByLabelText('What an agent can do with it'), {
      target: { value: 'List orders\nIssue a refund' },
    });
    fireEvent.change(screen.getByLabelText('Group'), { target: { value: 'payments' } });
    fireEvent.click(
      within(document.querySelector('.hx-own__editor') as HTMLElement).getByRole('button', { name: 'Save' }),
    );
    await waitFor(() => expect(p.onDescribe).toHaveBeenCalledTimes(1));
    expect(p.onDescribe).toHaveBeenCalledWith('m9', {
      name: 'Acme orders',
      category: 'payments',
      letter: 'A',
      about: 'Orders and refunds of the shop.',
      aboutRu: '',
      can: ['List orders', 'Issue a refund'],
      canRu: [],
      steps: ['Ask the platform team for a key'],
      stepsRu: [],
    });
  });

  it("is asked for with the agent: a ready request lands in a new chat's field", () => {
    render(<McpSection {...props({ mcp: { servers: [], catalog: [] } })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Connect with the agent' }));
    const draft = draftText(NEW_CHAT);
    expect(draft).toContain('connection_add');
    expect(draft).toContain('harnessly secret ask');
  });

  it("is made by hand: an API takes its key in a field that goes to the safe, never into the request's input", async () => {
    const p = props({ mcp: { servers: [], catalog: [] } });
    render(<McpSection {...p} />);
    fireEvent.click(screen.getByRole('button', { name: /^Add by hand/ }));
    fireEvent.click(screen.getByRole('button', { name: 'API with a key' }));
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Acme orders' } });
    fireEvent.change(screen.getByLabelText("The API's base address"), {
      target: { value: 'https://api.acme.example/v2' },
    });
    fireEvent.change(screen.getByLabelText('Check path (optional)'), { target: { value: '/me' } });
    fireEvent.change(screen.getByLabelText('API key (optional)'), {
      target: { value: TYPED_KEY },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
    await waitFor(() => expect(p.onAddOwn).toHaveBeenCalledTimes(1));
    const [input, values] = (p.onAddOwn as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(input).toEqual({
      kind: 'api',
      name: 'Acme orders',
      url: 'https://api.acme.example/v2',
      keys: [{ name: 'API_KEY', label: 'API key', optional: true }],
      apiHeaders: { Authorization: 'Bearer {API_KEY}' },
      checkPath: '/me',
    });
    expect(JSON.stringify(input)).not.toContain('sk-typed');
    expect(values).toEqual({ API_KEY: TYPED_KEY });
  });

  it("a local server waits for the person's explicit yes", () => {
    const p = props({ mcp: { servers: [], catalog: [] } });
    render(<McpSection {...p} />);
    fireEvent.click(screen.getByRole('button', { name: /^Add by hand/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Local server' }));
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Mine' } });
    fireEvent.change(screen.getByLabelText('Start command'), { target: { value: 'npx -y mine-mcp@1.2.3' } });
    const go = screen.getByRole('button', { name: 'Connect and start' });
    expect((go as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole('checkbox'));
    expect((go as HTMLButtonElement).disabled).toBe(false);
  });

  it('cuts a command into words and places the key in the header template', () => {
    expect(commandWords('  npx  -y  pkg@1.0.0 ')).toEqual(['npx', '-y', 'pkg@1.0.0']);
    expect(apiHeaderTemplate('Bearer ')).toBe('Bearer {API_KEY}');
  });
});
