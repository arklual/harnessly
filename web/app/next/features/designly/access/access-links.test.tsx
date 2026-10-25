// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import { t } from '@/next/i18n';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import type { IGuestLink, IInvite, IProjectAccess } from '@/next/features/designly/access/api';

const toast = vi.hoisted(() => vi.fn());
vi.mock('@/next/ui/toast', () => ({ toast }));

const written = vi.fn();

beforeEach(() => {
  written.mockReset();
  toast.mockReset();
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: written.mockResolvedValue(undefined) },
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const host = createRef<HTMLDivElement>();
const inRoot = (ui: React.ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

const json = (body: unknown, status = 200) =>
  Promise.resolve(
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }),
  );

const owner: IProjectAccess = {
  role: 'owner',
  owner: 'boss@x.ru',
  ownerName: 'Boss',
  team: 'view',
  canManage: true,
  editorsCanShare: false,
  request: null,
  people: [],
  invites: [],
  requests: [],
};

const link = (over: Partial<IGuestLink> = {}): IGuestLink => ({
  id: 'g1',
  title: 'Client',
  rights: 'view',
  expiresAt: '2099-01-01T00:00:00Z',
  revokedAt: null,
  opens: 3,
  createdAt: '2026-10-20T10:00:00Z',
  url: 'https://acme.example/s/tok1',
  comments: 2,
  ...over,
});

const props = (access: IProjectAccess) => ({
  open: true,
  onClose: vi.fn(),
  project: 'p1',
  title: 'Landing',
  access,
  onNext: vi.fn(),
  onCopyLink: vi.fn(),
  onClaim: vi.fn().mockResolvedValue(undefined),
});

const calls = (mock: ReturnType<typeof vi.fn>) =>
  (mock.mock.calls as unknown as [string, RequestInit | undefined][]).map(([url, init]) => [
    url,
    init?.method ?? 'GET',
    init?.body ? JSON.parse(init.body as string) : undefined,
  ]);

/** A gateway with the guest links of one project; changes answer as the real one would. */
const gateway = (start: IGuestLink[]) => {
  const state = [...start];
  return vi.fn((url: string, init?: RequestInit) => {
    if (url.includes('/invites') && init?.method === 'POST')
      return json({ id: 'n1', token: 'tok', url: 'https://x.ru/i/tok', expiresAt: '2099-01-01T00:00:00Z' });
    if (!url.includes('/shares')) return json({ people: [], log: [], ...owner });
    const method = init?.method ?? 'GET';
    const body = init?.body ? JSON.parse(init.body as string) : {};
    if (method === 'POST') {
      const made = link({
        id: 'g2',
        title: body.title,
        rights: body.rights,
        url: 'https://acme.example/s/tok2',
      });
      state.unshift(made);
      return json(made, 201);
    }
    if (method === 'PATCH') {
      const i = state.findIndex((l) => url.endsWith(`/${l.id}`));
      state[i] = {
        ...state[i]!,
        ...(body.rights ? { rights: body.rights } : {}),
        expiresAt: body.ttl ? '2099-06-01T00:00:00Z' : state[i]!.expiresAt,
      };
      return json(state[i]);
    }
    if (method === 'DELETE') return json({ ok: true });
    return json(state);
  });
};

const invite: IInvite = {
  id: 'i1',
  title: 'Agency',
  role: 'view',
  roleTtl: null,
  expiresAt: '2099-01-01T00:00:00Z',
  maxUses: 5,
  uses: 1,
  revokedAt: null,
};

const openLinks = async () => {
  fireEvent.click(await screen.findByRole('button', { name: t('next.access.linkSettings') }));
  return screen.findByText(t('next.access.linkNew'));
};

const VIEW_KEY = 'hx-next-dl-view:p1';

describe('the links screen', () => {
  beforeEach(() => localStorage.removeItem(VIEW_KEY));

  it('is titled «Ссылки» and lists both kinds in one list, each saying who it opens for', async () => {
    vi.stubGlobal('fetch', gateway([link()]));
    inRoot(<ShareDialog {...props({ ...owner, invites: [invite] })} />);
    await openLinks();
    expect(screen.getByRole('heading', { name: t('next.access.linksTitle') })).toBeTruthy();
    expect(await screen.findByText('Client')).toBeTruthy();
    expect(screen.getByText('Agency')).toBeTruthy();
    expect(screen.getByText(new RegExp(`^${t('next.access.linkKind.guest')} · `))).toBeTruthy();
    expect(screen.getByText(new RegExp(`^${t('next.access.linkKind.member')} · `))).toBeTruthy();
    expect(screen.getByText('Client').closest('ul')?.textContent).toContain('Agency');
    expect(screen.getAllByRole('button', { name: t('next.access.linkMake') })).toHaveLength(1);
  });

  it('has only the invite form where the gateway has no such links', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => json({ error: 'nope' }, 404)),
    );
    inRoot(<ShareDialog {...props(owner)} />);
    await openLinks();
    expect(screen.queryByRole('radiogroup', { name: t('next.access.linkWho') })).toBeNull();
    expect(screen.getByPlaceholderText(t('next.access.places'))).toBeTruthy();
  });

  it('makes a link without sign-in, copies it and sends the look the owner gave the design', async () => {
    localStorage.setItem(
      VIEW_KEY,
      JSON.stringify({
        v: 1,
        device: 'ip15',
        size: null,
        rot: false,
        chrome: true,
        browser: 'safari',
        fit: 'auto',
        zoom: { s: 1, x: 0, y: 0 },
        locked: true,
        box: null,
        page: 'Main.dc.html',
      }),
    );
    const fetchMock = gateway([]);
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...props(owner)} />);
    await openLinks();
    await screen.findByRole('radiogroup', { name: t('next.access.linkWho') });
    fireEvent.change(screen.getByLabelText(t('next.access.linkTitle')), { target: { value: 'Agency' } });
    fireEvent.click(screen.getByRole('button', { name: t('next.access.linkMake') }));
    await waitFor(() => expect(written).toHaveBeenCalledWith('https://acme.example/s/tok2'));
    const post = calls(fetchMock).find(
      ([url, method]) => String(url).includes('/p1/shares') && method === 'POST',
    );
    expect(post?.[2]).toEqual({
      title: 'Agency',
      rights: 'view',
      ttl: '7d',
      view: {
        v: 1,
        device: 'ip15',
        rotated: false,
        width: 393,
        height: 852,
        chrome: true,
        browser: 'safari',
        fit: 'auto',
        zoom: null,
        panX: null,
        panY: null,
        locked: true,
        page: 'Main.dc.html',
      },
    });
    await waitFor(() => expect(toast).toHaveBeenCalledWith(t('next.access.copied')));
    expect(await screen.findByText('Agency')).toBeTruthy();
  });

  it('sends no view for a design with nothing remembered', async () => {
    const fetchMock = gateway([]);
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...props(owner)} />);
    await openLinks();
    await screen.findByRole('radiogroup', { name: t('next.access.linkWho') });
    fireEvent.click(screen.getByRole('button', { name: t('next.access.linkMake') }));
    await waitFor(() => expect(written).toHaveBeenCalled());
    const post = calls(fetchMock).find(
      ([url, method]) => String(url).includes('/p1/shares') && method === 'POST',
    );
    expect(post?.[2]).toEqual({ title: '', rights: 'view', ttl: '7d' });
  });

  it('makes an invite for the signed-in from the same form and copies it', async () => {
    const fetchMock = gateway([]);
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...props(owner)} />);
    await openLinks();
    fireEvent.click(await screen.findByRole('radio', { name: t('next.access.linkWho.member') }));
    fireEvent.change(screen.getByPlaceholderText(t('next.access.places')), { target: { value: '3' } });
    fireEvent.click(screen.getByRole('button', { name: t('next.access.linkMake') }));
    await waitFor(() => expect(written).toHaveBeenCalledWith('https://x.ru/i/tok'));
    expect(calls(fetchMock)).toContainEqual([
      expect.stringContaining('/p1/invites'),
      'POST',
      { title: '', role: 'view', roleTtl: null, ttl: '7d', maxUses: 3 },
    ]);
  });

  it('extends a link by 7 days and changes what it allows', async () => {
    const fetchMock = gateway([link()]);
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...props(owner)} />);
    await openLinks();
    await screen.findByText('Client');
    fireEvent.click(screen.getByRole('button', { name: t('next.access.guest.actions') }));
    fireEvent.click(
      within(await screen.findByRole('menu')).getByRole('menuitem', { name: t('next.access.guest.extend') }),
    );
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/p1/shares/g1'),
        'PATCH',
        { ttl: '7d' },
      ]),
    );
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull());
    fireEvent.click(screen.getByRole('button', { name: t('next.access.guest.actions') }));
    fireEvent.click(
      within(await screen.findByRole('menu')).getByRole('menuitem', {
        name: t('next.access.guest.make.comment'),
      }),
    );
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/p1/shares/g1'),
        'PATCH',
        { rights: 'comment' },
      ]),
    );
    expect(await screen.findByText(new RegExp(`${t('next.access.can.comment')} · `))).toBeTruthy();
  });

  it('copies a link again and revokes it', async () => {
    const fetchMock = gateway([link()]);
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...props(owner)} />);
    await openLinks();
    await screen.findByText('Client');
    fireEvent.click(screen.getByRole('button', { name: t('next.access.copy') }));
    await waitFor(() => expect(written).toHaveBeenCalledWith('https://acme.example/s/tok1'));
    fireEvent.click(screen.getByRole('button', { name: t('next.access.guest.actions') }));
    fireEvent.click(
      within(await screen.findByRole('menu')).getByRole('menuitem', { name: t('next.access.revoke') }),
    );
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/p1/shares/g1'),
        'DELETE',
        undefined,
      ]),
    );
    await waitFor(() => expect(screen.queryByText('Client')).toBeNull());
  });

  it('says the gateway words when making a link is refused', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string, init?: RequestInit) =>
        url.includes('/shares')
          ? init?.method === 'POST'
            ? json({ error: 'Только владелец', code: 'forbidden' }, 403)
            : json([])
          : json(owner),
      ),
    );
    inRoot(<ShareDialog {...props(owner)} />);
    await openLinks();
    await screen.findByRole('radiogroup', { name: t('next.access.linkWho') });
    fireEvent.click(screen.getByRole('button', { name: t('next.access.linkMake') }));
    expect(await screen.findByText('Только владелец')).toBeTruthy();
  });
});
