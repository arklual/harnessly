// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import { t } from '@/next/i18n';
import ResourceShare from '@/next/features/designly/access/resource-share';
import { LINK_KIND } from '@/next/features/designly/access/link-form';
import type { IAccessLink, IResourceView } from '@/next/features/designly/access/resource-api';

const toast = vi.hoisted(() => vi.fn());
vi.mock('@/next/ui/toast', () => ({ toast }));
vi.mock('@/next/features/designly/sdk/toast', () => ({ toast }));

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

const link = (over: Partial<IAccessLink> = {}): IAccessLink => ({
  id: 'l1',
  forWhom: 'Иван, инвестор',
  rights: 'view',
  expiresAt: null,
  disabledAt: null,
  opens: 0,
  lastOpenedAt: null,
  createdAt: '2026-11-01T10:00:00Z',
  createdBy: 'boss@x.ru',
  comments: 0,
  options: {},
  version: 'pinned',
  pages: null,
  copyable: true,
  ...over,
});

const view = (over: Partial<IResourceView> = {}): IResourceView => ({
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
  grants: [],
  requests: [],
  links: [link()],
  myRequest: null,
  ...over,
});

const props = (v: IResourceView, over: Partial<React.ComponentProps<typeof ResourceShare>> = {}) => ({
  open: true,
  onClose: vi.fn(),
  target: { kind: 'design' as const, id: 'p1' },
  title: 'Landing',
  view: v,
  onNext: vi.fn(),
  address: 'https://ai.x.ru/d/p1',
  onCopyLink: vi.fn(),
  linkContext: { kind: LINK_KIND.design, pages: [{ path: 'index.dc.html', title: 'Главная' }] },
  ...over,
});

const calls = (mock: ReturnType<typeof vi.fn>) =>
  (mock.mock.calls as unknown as [string, RequestInit | undefined][]).map(([url, init]) => [
    url,
    init?.method ?? 'GET',
    init?.body ? JSON.parse(init.body as string) : undefined,
  ]);

const linksSection = () => screen.getByText(t('next.access.guestLinks')).closest('section')!;

describe('links without sign-in in the one dialog (HRN-675, stage «г»)', () => {
  it('lists the links with what they allow; a new one is made from its screen and copied at once', async () => {
    const made = link({ id: 'l2', forWhom: 'Марина', rights: 'comment', expiresAt: '2026-11-11T10:00:00Z' });
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url.endsWith('/links') && init?.method === 'POST')
        return json({ link: made, url: 'https://s.x.ru/s/TOKEN' }, 201);
      return json(view());
    });
    vi.stubGlobal('fetch', fetchMock);
    const onNext = vi.fn();
    inRoot(<ResourceShare {...props(view(), { onNext })} />);
    const section = linksSection();
    expect(within(section).getByText('Иван, инвестор')).toBeTruthy();
    expect(within(section).getByText(new RegExp(t('next.access.links.seenNever')))).toBeTruthy();

    fireEvent.click(within(section).getByRole('button', { name: t('next.access.linkNew') }));
    const who = await screen.findByRole('textbox', { name: t('next.access.links.whoLabel') });
    fireEvent.change(who, { target: { value: 'Марина' } });
    fireEvent.click(screen.getByRole('button', { name: new RegExp(t('next.access.links.rightsLabel')) }));
    fireEvent.click(
      screen.getByRole('radio', { name: new RegExp(t('next.access.links.rightsValue.comment')) }),
    );
    fireEvent.click(screen.getByRole('button', { name: t('next.access.links.create') }));

    await waitFor(() => expect(written).toHaveBeenCalledWith('https://s.x.ru/s/TOKEN'));
    const post = calls(fetchMock).find(([, method]) => method === 'POST');
    expect(post?.[0]).toContain('/access/design/p1/links');
    expect(post?.[2]).toEqual({
      forWhom: 'Марина',
      rights: 'comment',
      ttl: '7d',
      options: { device: 'fill' },
      version: 'pinned',
      pages: null,
    });
    await waitFor(() => expect(onNext).toHaveBeenCalled());
    expect((onNext.mock.calls.at(-1)?.[0] as IResourceView).links?.map((l) => l.id)).toEqual(['l2', 'l1']);
  });

  it('copies a kept address, switches a link off at once, and offers the new address where none was kept', async () => {
    const off = link({ disabledAt: '2026-11-04T10:00:00Z' });
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url.endsWith('/address') && (init?.method ?? 'GET') === 'GET')
        return json({ url: 'https://s.x.ru/s/KEPT' });
      if (init?.method === 'PATCH') return json({ link: off });
      return json(view());
    });
    vi.stubGlobal('fetch', fetchMock);
    const onNext = vi.fn();
    inRoot(<ResourceShare {...props(view(), { onNext })} />);
    const menuOf = async () => {
      fireEvent.click(within(linksSection()).getByRole('button', { name: t('next.access.guest.actions') }));
      return screen.findByRole('menu');
    };
    fireEvent.click(within(await menuOf()).getByRole('menuitem', { name: t('next.access.links.copy') }));
    await waitFor(() => expect(written).toHaveBeenCalledWith('https://s.x.ru/s/KEPT'));

    fireEvent.click(
      within(await menuOf()).getByRole('menuitem', { name: new RegExp(t('next.access.links.offDo')) }),
    );
    await waitFor(() => expect(onNext).toHaveBeenCalled());
    expect(calls(fetchMock).find(([, method]) => method === 'PATCH')).toEqual([
      expect.stringContaining('/access/design/p1/links/l1'),
      'PATCH',
      { disabled: true },
    ]);
    cleanup();

    inRoot(<ResourceShare {...props(view({ links: [link({ copyable: false })] }))} />);
    const menu = await menuOf();
    expect(within(menu).queryByRole('menuitem', { name: t('next.access.links.copy') })).toBeNull();
    expect(
      within(menu).getByRole('menuitem', { name: new RegExp(t('next.access.links.reissue')) }),
    ).toBeTruthy();
  });

  it("an editor who may share runs their own links only: someone else's has no menu", () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => json(view())),
    );
    const editor = view({ role: 'edit', manager: false, links: [link({ createdBy: 'someone@x.ru' })] });
    inRoot(<ResourceShare {...props(editor)} />);
    expect(within(linksSection()).getByText('Иван, инвестор')).toBeTruthy();
    expect(within(linksSection()).queryByRole('button', { name: t('next.access.guest.actions') })).toBeNull();
  });

  it('a document link: copying of the text, and «only on screen» when nothing may be taken', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => json(view())),
    );
    inRoot(
      <ResourceShare
        {...props(view({ links: [link({ options: { download: false, copy: false } })] }), {
          target: { kind: 'doc', id: 'spec' },
          linkContext: { kind: LINK_KIND.doc },
        })}
      />,
    );
    expect(within(linksSection()).getByText(new RegExp(t('next.access.links.noCopy')))).toBeTruthy();
    fireEvent.click(within(linksSection()).getByRole('button', { name: t('next.access.linkNew') }));
    const copy = await screen.findByRole('switch', { name: t('next.access.links.copyText') });
    expect(screen.queryByText(t('next.access.links.readOnly'))).toBeNull();
    fireEvent.click(copy);
    expect(screen.getByText(t('next.access.links.readOnly'))).toBeTruthy();
  });

  it('an older gateway without links in the view keeps the row of the older links', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => json(view())),
    );
    const older = view({ links: undefined });
    inRoot(
      <ResourceShare
        {...props(older, {
          links: { project: 'p1', access: {} as never, onNext: vi.fn() },
        })}
      />,
    );
    expect(screen.getByText(t('next.access.guestLinksOpen'))).toBeTruthy();
    expect(screen.queryByRole('button', { name: t('next.access.linkNew') })).toBeNull();
  });
});
