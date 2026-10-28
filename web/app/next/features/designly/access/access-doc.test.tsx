// @vitest-environment jsdom
// The same dialog for a catalog document: its routes are the gateway's /docs/<slug>/acl, its links are only
// the ones without sign-in and cannot be changed in place, and it has no hand-over.
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import { t } from '@/next/i18n';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import {
  docRef,
  fetchGuestLinks,
  isDocRef,
  putTeam,
  type IProjectAccess,
} from '@/next/features/designly/access/api';

vi.mock('@/next/ui/toast', () => ({ toast: vi.fn() }));

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
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
  team: 'none',
  canManage: true,
  editorsCanShare: false,
  request: null,
  people: [],
  requests: [],
};

const SLUG = 'plan-abc';
const urls = (mock: ReturnType<typeof vi.fn>) =>
  (mock.mock.calls as unknown as [string][]).map(([url]) => url);

describe('a catalog document in the access dialog', () => {
  it('is told from a design by its reference', () => {
    expect(docRef(SLUG)).toBe('doc:plan-abc');
    expect(isDocRef(docRef(SLUG))).toBe(true);
    expect(isDocRef('ostrovok-dossier')).toBe(false);
  });

  it("changes the team level on the document's acl route", async () => {
    const fetchMock = vi.fn(() => json(owner));
    vi.stubGlobal('fetch', fetchMock);
    await putTeam(docRef(SLUG), 'view');
    const [url, init] = (fetchMock.mock.calls as unknown as [string, RequestInit][])[0]!;
    expect(url).toContain('/docs/plan-abc/acl/team');
    expect(url).not.toContain('design/access');
    expect(init.method).toBe('PUT');
  });

  it('reads the links as the gateway lists them, a link without an address too', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        json({
          shares: [
            {
              id: 'g1',
              title: 'Customer',
              rights: 'view',
              expiresAt: null,
              revokedAt: null,
              opens: 1,
              comments: 0,
            },
          ],
        }),
      ),
    );
    const links = await fetchGuestLinks(docRef(SLUG));
    expect(links).toHaveLength(1);
    expect(links?.[0]).toMatchObject({ id: 'g1', url: '' });
  });

  it('shows the links without a copy button or a change menu item, and offers no hand-over', async () => {
    const fetchMock = vi.fn((url: string) =>
      url.includes('/shares')
        ? json({
            shares: [
              {
                id: 'g1',
                title: 'Customer',
                rights: 'view',
                expiresAt: null,
                revokedAt: null,
                opens: 1,
                comments: 0,
              },
            ],
          })
        : json(owner),
    );
    vi.stubGlobal('fetch', fetchMock);
    inRoot(
      <ShareDialog
        open
        onClose={vi.fn()}
        project={docRef(SLUG)}
        title="plan.md"
        access={owner}
        onNext={vi.fn()}
        onCopyLink={vi.fn()}
        onClaim={vi.fn().mockResolvedValue(undefined)}
        footExtra={<button type="button">Unlink</button>}
      />,
    );
    expect(await screen.findByRole('button', { name: 'Unlink' })).toBeTruthy();

    // The «…» menu of the head: the journal and the link settings, no «close to all» and no hand-over.
    fireEvent.click(screen.getByRole('button', { name: t('next.access.more') }));
    expect(await screen.findByRole('menuitem', { name: t('next.access.history') })).toBeTruthy();
    expect(screen.queryByRole('menuitem', { name: t('next.access.transferItem') })).toBeNull();
    expect(screen.queryByRole('menuitem', { name: t('next.access.revokeAll') })).toBeNull();

    fireEvent.keyDown(document.body, { key: 'Escape' });
    fireEvent.click(await screen.findByRole('button', { name: t('next.access.linkSettings') }));
    expect(await screen.findByText('Customer')).toBeTruthy();
    expect(screen.queryByRole('button', { name: t('next.access.copy') })).toBeNull();
    // Only the links without sign-in: no choice of who, no invitations.
    expect(screen.queryByRole('radiogroup', { name: t('next.access.linkWho') })).toBeNull();
    expect(screen.queryByPlaceholderText(t('next.access.places'))).toBeNull();
    await waitFor(() =>
      expect(urls(fetchMock).some((u) => u.includes('/docs/plan-abc/acl/shares'))).toBe(true),
    );
  });
});
