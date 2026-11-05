// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import { t } from '@/next/i18n';
import ResourceShare from '@/next/features/designly/access/resource-share';
import { fetchView, type IResourceView } from '@/next/features/designly/access/resource-api';

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

beforeEach(() => {
  // The wide window: a modal and popovers, not the phone's sheets.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
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

const owner: IResourceView = {
  resource: { kind: 'design', id: 'p1', subkind: 'site', title: 'Landing' },
  stored: true,
  role: 'owner',
  until: null,
  canManage: true,
  manager: true,
  steward: false,
  owner: { email: 'boss@x.ru', name: 'Boss' },
  homeTeam: { teamId: 't1', name: 'Design', role: 'edit' },
  editorsCanShare: true,
  grants: [
    {
      type: 'user',
      id: 'ann@x.ru',
      name: 'Ann',
      sub: 'ann@x.ru',
      role: 'edit',
      canShare: false,
      expiresAt: null,
    },
    {
      type: 'team',
      id: 't2',
      name: 'Marketing',
      sub: 'Acme · 4',
      role: 'view',
      canShare: false,
      expiresAt: null,
    },
  ],
  requests: [
    {
      id: 'r1',
      email: 'bob@x.ru',
      name: 'Bob',
      level: 'edit',
      message: 'need it',
      createdAt: '2026-11-01T10:00:00Z',
    },
  ],
  myRequest: null,
};

const viewer: IResourceView = {
  ...owner,
  role: 'view',
  canManage: false,
  manager: false,
  grants: undefined,
  requests: undefined,
};

const props = (view: IResourceView, over: Partial<React.ComponentProps<typeof ResourceShare>> = {}) => ({
  open: true,
  onClose: vi.fn(),
  target: { kind: 'design' as const, id: 'p1' },
  title: 'Landing',
  view,
  onNext: vi.fn(),
  address: 'https://ai.x.ru/d/p1',
  onCopyLink: vi.fn(),
  ...over,
});

/** The calls fetch got, as [url, method, body]. */
const calls = (mock: ReturnType<typeof vi.fn>) =>
  (mock.mock.calls as unknown as [string, RequestInit | undefined][]).map(([url, init]) => [
    url,
    init?.method ?? 'GET',
    init?.body ? JSON.parse(init.body as string) : undefined,
  ]);

const openMenu = async (name: string | RegExp) => {
  fireEvent.click(await screen.findByRole('button', { name }));
  return screen.findByRole('menu');
};

describe('the one «Поделиться»', () => {
  it('shows the field, the owner, the home team first, the grants and the requests', async () => {
    inRoot(<ResourceShare {...props(owner)} />);
    expect(await screen.findByText(t('next.access.shareTitle', { name: 'Landing' }))).toBeTruthy();
    expect(screen.getByRole('combobox', { name: t('next.access.addWho') })).toBeTruthy();
    const people = screen.getByText(t('next.access.whoHas')).closest('section')!;
    const rows = within(people).getAllByRole('listitem');
    expect(within(rows[0]!).getByText('Boss')).toBeTruthy();
    expect(within(rows[1]!).getByText(t('next.access.teamName', { name: 'Design' }))).toBeTruthy();
    expect(within(people).getByText('Ann')).toBeTruthy();
    expect(within(people).getByText('Marketing')).toBeTruthy();
    expect(screen.getByText('Bob')).toBeTruthy();
    expect(screen.getByRole('button', { name: t('next.access.copyLink') })).toBeTruthy();
    expect(screen.getByRole('button', { name: t('next.access.done') })).toBeTruthy();
  });

  it('turns picks into chips, takes the last one back with Backspace, and invites each with one role', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.includes('/access/directory'))
        return json({ people: [{ email: 'kate@x.ru', name: 'Kate', teams: [] }], teams: [] });
      return json(owner);
    });
    vi.stubGlobal('fetch', fetchMock);
    const onNext = vi.fn();
    inRoot(<ResourceShare {...props(owner, { onNext })} />);
    const field = await screen.findByRole('combobox', { name: t('next.access.addWho') });
    fireEvent.change(field, { target: { value: 'ka' } });
    fireEvent.click(await screen.findByRole('option', { name: /Kate/ }));
    fireEvent.change(field, { target: { value: 'new@x.ru' } });
    fireEvent.click(
      await screen.findByRole('option', { name: t('next.access.inviteEmail', { email: 'new@x.ru' }) }),
    );
    fireEvent.change(field, { target: { value: 'gone@x.ru' } });
    fireEvent.click(
      await screen.findByRole('option', { name: t('next.access.inviteEmail', { email: 'gone@x.ru' }) }),
    );
    expect(screen.getByText('gone@x.ru')).toBeTruthy();
    fireEvent.keyDown(field, { key: 'Backspace' });
    expect(screen.queryByText('gone@x.ru')).toBeNull();
    const menu = await openMenu(t('next.access.newRole'));
    fireEvent.click(within(menu).getByRole('menuitemradio', { name: t('next.access.can.edit') }));
    fireEvent.click(screen.getByRole('button', { name: t('next.access.invite') }));
    await waitFor(() => expect(onNext).toHaveBeenCalledWith(owner));
    const puts = calls(fetchMock).filter(([, method]) => method === 'PUT');
    expect(puts).toEqual([
      [
        expect.stringContaining('/access/design/p1/grants/user/kate%40x.ru'),
        'PUT',
        { role: 'edit', canShare: false, ttl: null },
      ],
      [
        expect.stringContaining('/access/design/p1/grants/user/new%40x.ru'),
        'PUT',
        { role: 'edit', canShare: false, ttl: null },
      ],
    ]);
    expect(screen.queryByText('Kate')).toBeNull();
  });

  it('removes the home team from its menu and offers it back', async () => {
    const gone: IResourceView = { ...owner, homeTeam: { teamId: 't1', name: 'Design', role: null } };
    const fetchMock = vi.fn(() => json(gone));
    vi.stubGlobal('fetch', fetchMock);
    const onNext = vi.fn();
    const view = inRoot(<ResourceShare {...props(owner, { onNext })} />);
    const team = t('next.access.teamName', { name: 'Design' });
    const menu = await openMenu(t('next.access.roleOf', { who: team }));
    fireEvent.click(within(menu).getByRole('menuitem', { name: t('next.access.teamRemove') }));
    await waitFor(() => expect(onNext).toHaveBeenCalledWith(gone));
    expect(calls(fetchMock)[0]).toEqual([
      expect.stringContaining('/access/design/p1/team'),
      'PUT',
      { role: null },
    ]);
    view.rerender(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <ResourceShare {...props(gone, { onNext })} />
      </PortalContext.Provider>,
    );
    expect(await screen.findByText(t('next.access.teamGone', { name: team }))).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: t('next.access.teamBack') }));
    await waitFor(() =>
      expect(calls(fetchMock)[1]).toEqual([
        expect.stringContaining('/access/design/p1/team'),
        'PUT',
        { role: 'edit' },
      ]),
    );
  });

  it('offers «Может делиться» at edit to a manager and sends it with the role', async () => {
    const fetchMock = vi.fn(() => json(owner));
    vi.stubGlobal('fetch', fetchMock);
    inRoot(<ResourceShare {...props(owner)} />);
    const menu = await openMenu(t('next.access.roleOf', { who: 'Ann' }));
    fireEvent.click(within(menu).getByRole('menuitemcheckbox', { name: t('next.access.mayShare') }));
    await waitFor(() =>
      expect(calls(fetchMock)).toContainEqual([
        expect.stringContaining('/access/design/p1/grants/user/ann%40x.ru'),
        'PUT',
        { role: 'edit', canShare: true, ttl: null },
      ]),
    );
  });

  it('keeps one who may share out of reach of an editor who is not a manager', async () => {
    const editor: IResourceView = {
      ...owner,
      role: 'edit',
      manager: false,
      grants: [{ ...owner.grants![0]!, canShare: true }],
    };
    inRoot(<ResourceShare {...props(editor)} />);
    expect(
      await screen.findByText(`${t('next.access.can.edit')} · ${t('next.access.mayShareShort')}`),
    ).toBeTruthy();
    expect(screen.queryByRole('button', { name: t('next.access.roleOf', { who: 'Ann' }) })).toBeNull();
  });

  it('greys «Передать владение» for one who is neither the owner nor a steward, and says why', async () => {
    inRoot(<ResourceShare {...props({ ...owner, role: 'edit' })} />);
    const menu = await openMenu(t('next.access.more'));
    const item = within(menu).getByRole('menuitem', { name: new RegExp(t('next.access.transferItem')) });
    expect(item.getAttribute('aria-disabled')).toBe('true');
    expect(within(menu).getByText(t('next.access.transferOnlyOwner', { name: 'Boss' }))).toBeTruthy();
    expect(within(menu).queryByRole('menuitemcheckbox')).toBeNull();
  });

  it("puts a document's extra block after the links and its foot button after «Копировать ссылку»", async () => {
    inRoot(
      <ResourceShare
        {...props(owner, {
          target: { kind: 'doc' as const, id: 'spec' },
          extra: <section data-testid="legacy">old public link</section>,
          footExtra: <button type="button">Отвязать</button>,
        })}
      />,
    );
    expect(await screen.findByTestId('legacy')).toBeTruthy();
    const foot = screen.getByRole('button', { name: t('next.access.copyLink') }).parentElement!;
    expect(within(foot).getByRole('button', { name: 'Отвязать' })).toBeTruthy();
  });

  it('gives one who may only look the address, the copy and «Попросить правку»', async () => {
    const asked: IResourceView = { ...viewer, myRequest: { id: 'r9', level: 'edit', status: 'open' } };
    let answer: (v: Response) => void = () => undefined;
    const fetchMock = vi.fn(() => new Promise<Response>((resolve) => (answer = resolve)));
    vi.stubGlobal('fetch', fetchMock);
    const onNext = vi.fn();
    const onCopyLink = vi.fn();
    inRoot(<ResourceShare {...props(viewer, { onNext, onCopyLink })} />);
    expect(await screen.findByText('https://ai.x.ru/d/p1')).toBeTruthy();
    expect(screen.getByText(t('next.access.youCan.view'))).toBeTruthy();
    expect(screen.queryByRole('combobox')).toBeNull();
    expect(screen.queryByRole('button', { name: t('next.access.more') })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: t('next.access.copyLink') }));
    expect(onCopyLink).toHaveBeenCalled();
    // «Попросить правку» opens the request here: the level (edit first) and a word to the owner.
    fireEvent.click(screen.getByRole('button', { name: t('next.access.askEditShort') }));
    expect(fetchMock).not.toHaveBeenCalled();
    fireEvent.change(await screen.findByLabelText(t('next.access.message')), {
      target: { value: 'Поправлю тексты' },
    });
    fireEvent.click(screen.getByRole('button', { name: t('next.access.ask') }));
    expect(calls(fetchMock)[0]).toEqual([
      expect.stringContaining('/access/design/p1/requests'),
      'POST',
      { level: 'edit', message: 'Поправлю тексты' },
    ]);
    // Pessimistic: nothing is taken for sent until the gateway answers.
    expect(onNext).not.toHaveBeenCalled();
    await act(async () => answer(await json(asked)));
    await waitFor(() => expect(onNext).toHaveBeenCalledWith(asked));
  });

  it('opened to ask (the viewing mode), has the request form open; a sent request says so, a denied one asks again', async () => {
    const fetchMock = vi.fn(() => json(viewer));
    vi.stubGlobal('fetch', fetchMock);
    const view = inRoot(<ResourceShare {...props(viewer)} ask />);
    fireEvent.click(await screen.findByRole('radio', { name: t('next.access.level.comment') }));
    fireEvent.click(screen.getByRole('button', { name: t('next.access.ask') }));
    await waitFor(() =>
      expect(calls(fetchMock)[0]).toEqual([
        expect.stringContaining('/access/design/p1/requests'),
        'POST',
        { level: 'comment', message: '' },
      ]),
    );
    const sent: IResourceView = { ...viewer, myRequest: { id: 'r1', level: 'comment', status: 'open' } };
    view.rerender(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <ResourceShare {...props(sent)} ask />
      </PortalContext.Provider>,
    );
    expect(await screen.findByText(t('next.access.askedOwner'))).toBeTruthy();
    expect(screen.queryByLabelText(t('next.access.message'))).toBeNull();
    const denied: IResourceView = { ...viewer, myRequest: { id: 'r1', level: 'comment', status: 'denied' } };
    view.rerender(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <ResourceShare {...props(denied)} ask />
      </PortalContext.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: t('next.access.askEditShort') }));
    expect(await screen.findByText(t('next.access.requestDenied'))).toBeTruthy();
  });
});

describe('the view of a resource', () => {
  it('is null where the gateway does not know the route yet, so the older dialog stays', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => json({ error: 'not found' }, 404)),
    );
    expect(await fetchView({ kind: 'design', id: 'p1' })).toBeNull();
  });

  it('is the view where it does', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => json(owner)),
    );
    expect(await fetchView({ kind: 'design', id: 'p1' })).toEqual(owner);
  });
});
