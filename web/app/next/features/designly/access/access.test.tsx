// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import { t } from '@/next/i18n';
import AccessLog from '@/next/features/designly/access/access-log';
import InviteAccept from '@/next/features/designly/access/invite-accept';
import NoAccess from '@/next/features/designly/access/no-access';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import TtlPicker from '@/next/features/designly/access/ttl-picker';
import { fetchAccess, type IProjectAccess } from '@/next/features/designly/access/api';
import { untilLabel } from '@/next/features/designly/access/labels';

afterEach(() => {
  cleanup();
  vi.useRealTimers();
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

const manager: IProjectAccess = {
  role: 'owner',
  owner: 'boss@x.ru',
  ownerName: 'Boss',
  team: 'view',
  canManage: true,
  editorsCanShare: false,
  request: null,
  people: [
    {
      email: 'ann@x.ru',
      role: 'edit',
      grantedBy: 'boss@x.ru',
      grantedAt: '2026-10-20T10:00:00Z',
      expiresAt: null,
      viaInvite: null,
    },
  ],
  invites: [
    {
      id: 'i1',
      title: 'Agency',
      role: 'view',
      roleTtl: '7d',
      expiresAt: '2099-01-01T00:00:00Z',
      maxUses: 5,
      uses: 1,
      revokedAt: null,
    },
  ],
  requests: [
    {
      id: 'r1',
      email: 'bob@x.ru',
      level: 'edit',
      message: 'need it',
      status: 'open',
      createdAt: '2026-10-24T10:00:00Z',
    },
  ],
};

const shareProps = (
  access: IProjectAccess,
  over: Partial<React.ComponentProps<typeof ShareDialog>> = {},
) => ({
  open: true,
  onClose: vi.fn(),
  project: 'p1',
  title: 'Landing',
  access,
  onNext: vi.fn(),
  onCopyLink: vi.fn(),
  onClaim: vi.fn().mockResolvedValue(undefined),
  ...over,
});

/** The calls fetch got, as [url, method, body]. */
const calls = (mock: ReturnType<typeof vi.fn>) =>
  (mock.mock.calls as unknown as [string, RequestInit | undefined][]).map(([url, init]) => [
    url,
    init?.method ?? 'GET',
    init?.body ? JSON.parse(init.body as string) : undefined,
  ]);

/** A gateway that answers every change with the manager's state (and a link where one is made). */
const gateway = () =>
  vi.fn((url: string, init?: RequestInit) => {
    if (url.includes('/invites') && init?.method === 'POST')
      return json({
        id: 'n1',
        token: 'tok_n1',
        url: 'https://x.ru/i/tok_n1',
        expiresAt: '2099-01-01T00:00:00Z',
      });
    if (url.endsWith('/log'))
      return json({
        log: [
          { id: 1, actor: 'boss@x.ru', action: 'grant', subject: 'ann@x.ru', at: '2026-10-20T10:00:00Z' },
        ],
      });
    if (url.includes('/docs/people')) return json({ people: [] });
    return json(manager);
  });

const openMenu = async (name: string | RegExp) => {
  fireEvent.click(await screen.findByRole('button', { name }));
  return screen.findByRole('menu');
};

describe('the share dialog', () => {
  it('shows the field, who has access, requests and general access; nothing dangerous on the main screen', async () => {
    inRoot(<ShareDialog {...shareProps(manager)} />);
    expect(await screen.findByText(t('next.access.shareTitle', { name: 'Landing' }))).toBeTruthy();
    expect(screen.getByLabelText(t('next.access.addField'))).toBeTruthy();
    expect(screen.getByText(t('next.access.whoHas'))).toBeTruthy();
    expect(screen.getByText('ann@x.ru')).toBeTruthy();
    expect(screen.getByText('Boss')).toBeTruthy();
    // The owner is the person: «Вы».
    expect(screen.getByText(t('next.access.you'))).toBeTruthy();
    expect(screen.getByText(t('next.access.requests'))).toBeTruthy();
    expect(screen.getByText('bob@x.ru')).toBeTruthy();
    expect(
      screen.getByRole('button', {
        name: `${t('next.access.general')}: ${t('next.access.general.view')}`,
      }),
    ).toBeTruthy();
    expect(screen.getByText(t('next.access.general.view.hint'))).toBeTruthy();
    expect(screen.getByRole('button', { name: t('next.access.copyLink') })).toBeTruthy();
    expect(screen.getByRole('button', { name: t('next.access.done') })).toBeTruthy();
    expect(screen.queryByText(t('next.access.revokeAll'))).toBeNull();
    expect(screen.queryByText(t('next.access.transferItem'))).toBeNull();
    expect(screen.queryByText('Agency')).toBeNull();
  });

  it('viewer sees only own role and request button', async () => {
    const viewer: IProjectAccess = {
      ...manager,
      role: 'view',
      canManage: false,
      people: undefined,
      invites: undefined,
      requests: undefined,
    };
    const onCopyLink = vi.fn();
    inRoot(<ShareDialog {...shareProps(viewer, { onCopyLink })} />);
    expect(
      await screen.findByText(t('next.access.yourRole', { role: t('next.access.role.view') })),
    ).toBeTruthy();
    expect(screen.getByRole('button', { name: t('next.access.ask') })).toBeTruthy();
    expect(screen.queryByText(t('next.access.whoHas'))).toBeNull();
    expect(screen.queryByRole('button', { name: t('next.access.more') })).toBeNull();
    // The design's own address: they may not invite.
    fireEvent.click(screen.getByRole('button', { name: t('next.access.copyLink') }));
    expect(onCopyLink).toHaveBeenCalled();
  });

  it('adds a person with the role and the term chosen in the field, after the gateway answers', async () => {
    let answer: (v: Response) => void = () => undefined;
    // Only the grant is held back; the person field's suggestions answer at once.
    const fetchMock = vi.fn((url: string) =>
      url.includes('/design/access/') ? new Promise<Response>((resolve) => (answer = resolve)) : json([]),
    );
    vi.stubGlobal('fetch', fetchMock);
    const onNext = vi.fn();
    inRoot(<ShareDialog {...shareProps(manager, { onNext })} />);
    const field = await screen.findByLabelText(t('next.access.addField'));
    fireEvent.change(field, { target: { value: 'New@x.ru' } });
    let menu = await openMenu(t('next.access.newRole'));
    fireEvent.click(within(menu).getByRole('menuitemradio', { name: t('next.access.can.edit') }));
    menu = await openMenu(t('next.access.newRole'));
    fireEvent.click(within(menu).getByRole('menuitem', { name: new RegExp(t('next.access.until')) }));
    fireEvent.click(within(menu).getByRole('menuitemradio', { name: t('next.access.ttl.week') }));
    fireEvent.click(screen.getByRole('button', { name: t('next.access.invite') }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [url, method, body] = calls(fetchMock).find(([u]) => String(u).includes('/people/'))!;
    expect(url).toContain('/api/platform/design/access/p1/people/new%40x.ru');
    expect(method).toBe('PUT');
    expect(body).toEqual({ role: 'edit', ttl: '7d' });
    // Nothing is shown and nothing is taken for done until the gateway has answered.
    expect(onNext).not.toHaveBeenCalled();
    const next = {
      ...manager,
      people: [
        ...(manager.people ?? []),
        { ...manager.people![0]!, email: 'new@x.ru', role: 'edit' as const },
      ],
    };
    await act(async () => answer(await json(next)));
    await waitFor(() => expect(onNext).toHaveBeenCalledWith(next));
  });

  it('says a half address is not one', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...shareProps(manager)} />);
    const field = await screen.findByLabelText(t('next.access.addField'));
    fireEvent.change(field, { target: { value: 'ann' } });
    fireEvent.submit(field.closest('form')!);
    expect(await screen.findByText(t('next.access.needEmail'))).toBeTruthy();
    expect(calls(fetchMock).some(([u]) => String(u).includes('/people/'))).toBe(false);
  });

  it('changes a role keeping its term, and removes access from the same menu', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...shareProps(manager)} />);
    let menu = await openMenu(t('next.access.roleOf', { who: 'ann@x.ru' }));
    fireEvent.click(within(menu).getByRole('menuitemradio', { name: t('next.access.can.comment') }));
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/people/ann%40x.ru'),
        'PUT',
        { role: 'comment', ttl: null },
      ]),
    );
    menu = await openMenu(t('next.access.roleOf', { who: 'ann@x.ru' }));
    fireEvent.click(within(menu).getByRole('menuitem', { name: t('next.access.removeAccess') }));
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/people/ann%40x.ru'),
        'DELETE',
        undefined,
      ]),
    );
  });

  it('shows the end of a role as «до 12 ноя»', async () => {
    const until: IProjectAccess = {
      ...manager,
      people: [{ ...manager.people![0]!, expiresAt: '2026-11-12T12:00:00Z' }],
    };
    inRoot(<ShareDialog {...shareProps(until)} />);
    expect(untilLabel('2026-11-12T12:00:00Z')).toMatch(/12/);
    expect(await screen.findByText(untilLabel('2026-11-12T12:00:00Z')!)).toBeTruthy();
  });

  it('changes the general access from its one row', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...shareProps(manager)} />);
    const menu = await openMenu(new RegExp(`^${t('next.access.general')}`));
    fireEvent.click(
      within(menu).getByRole('menuitemradio', { name: new RegExp(t('next.access.general.none')) }),
    );
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/p1/team'),
        'PUT',
        { level: 'none' },
      ]),
    );
  });

  it('copy link copies the design address and makes no invite, even for only invited people', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    const onCopyLink = vi.fn();
    inRoot(<ShareDialog {...shareProps({ ...manager, team: 'none' }, { onCopyLink })} />);
    fireEvent.click(await screen.findByRole('button', { name: t('next.access.copyLink') }));
    expect(onCopyLink).toHaveBeenCalledTimes(1);
    await act(async () => {});
    expect(calls(fetchMock).some(([u]) => String(u).includes('/invites'))).toBe(false);
  });

  it('closing access to everyone asks first, in the menu', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...shareProps(manager)} />);
    const menu = await openMenu(t('next.access.more'));
    fireEvent.click(within(menu).getByRole('menuitem', { name: t('next.access.revokeAll') }));
    expect(await screen.findByText(t('next.access.revokeAllAsk'))).toBeTruthy();
    expect(calls(fetchMock).some(([u]) => String(u).includes('revoke-all'))).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: t('next.access.revokeAllDo') }));
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([expect.stringContaining('/p1/revoke-all'), 'POST', {}]),
    );
  });

  it('handing the project over names the heir before it is sent', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...shareProps(manager)} />);
    const menu = await openMenu(t('next.access.more'));
    fireEvent.click(within(menu).getByRole('menuitem', { name: t('next.access.transferItem') }));
    fireEvent.change(await screen.findByLabelText(t('next.access.heir')), { target: { value: 'heir@x.ru' } });
    fireEvent.click(screen.getByRole('button', { name: t('next.access.transferNext') }));
    expect(screen.getByText(t('next.access.transferAsk', { who: 'heir@x.ru' }))).toBeTruthy();
    expect(calls(fetchMock).some(([u]) => String(u).endsWith('/owner'))).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: t('next.access.transferDo') }));
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/p1/owner'),
        'POST',
        { email: 'heir@x.ru' },
      ]),
    );
  });

  it('editors may share: a checkbox of the menu', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...shareProps(manager)} />);
    const menu = await openMenu(t('next.access.more'));
    const item = within(menu).getByRole('menuitemcheckbox', { name: t('next.access.editorsShare') });
    expect(item.getAttribute('aria-checked')).toBe('false');
    fireEvent.click(item);
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/p1/settings'),
        'PUT',
        { editorsCanShare: true },
      ]),
    );
  });

  it('the access history is a view with a way back', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...shareProps(manager)} />);
    const menu = await openMenu(t('next.access.more'));
    fireEvent.click(within(menu).getByRole('menuitem', { name: t('next.access.history') }));
    expect(await screen.findByRole('heading', { name: t('next.access.history') })).toBeTruthy();
    expect(await screen.findByText(/boss@x\.ru · дал доступ|boss@x\.ru · gave access/)).toBeTruthy();
    expect(calls(fetchMock)).toContainEqual([expect.stringContaining('/p1/log'), 'GET', undefined]);
    fireEvent.click(screen.getByRole('button', { name: t('next.access.backView') }));
    await waitFor(() => expect(screen.queryByRole('heading', { name: t('next.access.history') })).toBeNull());
  });

  it('the link settings list the links and revoke them', async () => {
    const fetchMock = gateway();
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ShareDialog {...shareProps(manager)} />);
    fireEvent.click(await screen.findByRole('button', { name: t('next.access.linkSettings') }));
    expect(await screen.findByText('Agency')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: t('next.access.revoke') }));
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/p1/invites/i1'),
        'DELETE',
        undefined,
      ]),
    );
  });

  it('says the gateway words when a change is refused', async () => {
    vi.stubGlobal('fetch', () => json({ error: 'Только владелец', code: 'forbidden' }, 403));
    inRoot(<ShareDialog {...shareProps(manager)} />);
    const menu = await openMenu(t('next.access.roleOf', { who: 'ann@x.ru' }));
    fireEvent.click(within(menu).getByRole('menuitem', { name: t('next.access.removeAccess') }));
    expect(await screen.findByText('Только владелец')).toBeTruthy();
  });
});

describe('the term picker', () => {
  it('ttl picker offers 1h 1d 7d 30d date forever', async () => {
    const onChange = vi.fn();
    inRoot(<TtlPicker label="Term" value="7d" onChange={onChange} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Term' }));
    const menu = await screen.findByRole('menu');
    const names = [...menu.querySelectorAll('[role^="menuitem"]')].map((x) => x.textContent);
    expect(names).toEqual([
      t('next.access.ttl.hour'),
      t('next.access.ttl.day'),
      t('next.access.ttl.week'),
      t('next.access.ttl.month'),
      t('next.access.ttl.date'),
      t('next.access.ttl.forever'),
    ]);
    fireEvent.click(within(menu).getByText(t('next.access.ttl.forever')));
    expect(onChange).toHaveBeenCalledWith(null);
  });
});

describe('no access', () => {
  it('no access card sends a request and shows sent', async () => {
    const stranger: IProjectAccess = {
      role: null,
      owner: null,
      ownerName: 'Boss',
      team: 'none',
      canManage: false,
      editorsCanShare: false,
      request: null,
    };
    const sent = {
      ...stranger,
      request: {
        id: 'r',
        email: 'me@x.ru',
        level: 'edit' as const,
        message: 'hi',
        status: 'open' as const,
        createdAt: '2026-10-25T10:00:00Z',
      },
    };
    const fetchMock = vi.fn(() => json(sent));
    vi.stubGlobal('fetch', fetchMock);
    const onAsked = vi.fn();
    const view = render(<NoAccess project="p1" access={stranger} onAsked={onAsked} onBack={vi.fn()} />);
    expect(screen.getByText(t('next.access.owner', { name: 'Boss' }))).toBeTruthy();
    fireEvent.click(screen.getByRole('radio', { name: t('next.access.level.edit') }));
    fireEvent.change(screen.getByLabelText(t('next.access.message')), { target: { value: 'hi' } });
    fireEvent.click(screen.getByRole('button', { name: t('next.access.ask') }));
    await waitFor(() => expect(onAsked).toHaveBeenCalledWith(sent));
    const call = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(call[1].body as string)).toEqual({ level: 'edit', message: 'hi' });
    view.rerender(<NoAccess project="p1" access={sent} onAsked={onAsked} onBack={vi.fn()} />);
    expect(screen.getByText(t('next.access.requestSent'))).toBeTruthy();
  });
});

describe('an invite link', () => {
  it('invite accept opens the project', async () => {
    const fetchMock = vi.fn(() => json({ project: 'p1', role: 'view', until: null }));
    vi.stubGlobal('fetch', fetchMock);
    const onOpen = vi.fn();
    render(<InviteAccept token="tok_12345678" onOpen={onOpen} />);
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith('p1'));
    expect((fetchMock.mock.calls[0] as unknown as [string, RequestInit])[0]).toContain(
      '/api/platform/design/access/invites/tok_12345678/accept',
    );
  });

  it('says a spent link is gone', async () => {
    vi.stubGlobal('fetch', () => json({ error: 'Ссылка истекла', code: 'gone' }, 410));
    render(<InviteAccept token="tok_12345678" onOpen={vi.fn()} />);
    expect(await screen.findByText(t('next.access.inviteGoneTitle'))).toBeTruthy();
  });
});

describe('waiting', () => {
  it('loading is a skeleton after 150 ms, not empty', async () => {
    vi.useFakeTimers();
    vi.stubGlobal('fetch', () => new Promise(() => undefined));
    const { container } = render(<AccessLog project="p1" />);
    await act(() => vi.advanceTimersByTimeAsync(100));
    expect(container.querySelector('.hx-sk')).toBeNull();
    await act(() => vi.advanceTimersByTimeAsync(60));
    expect(container.querySelector('.hx-sk')).not.toBeNull();
    // Not read yet is not the same as nothing in it.
    expect(screen.queryByText(t('next.access.log.empty'))).toBeNull();
  });
});

describe('without a gateway', () => {
  it('reads as no access state, quietly', async () => {
    vi.stubGlobal('fetch', () => Promise.resolve(new Response('<html>', { status: 404 })));
    expect(await fetchAccess('p1')).toBeNull();
    vi.stubGlobal('fetch', () => Promise.reject(new TypeError('offline')));
    expect(await fetchAccess('p1')).toBeNull();
  });
});

beforeEach(() => {
  vi.restoreAllMocks();
  // The wide window: the menus are popovers, not the phone's sheets.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
