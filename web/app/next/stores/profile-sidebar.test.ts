import { beforeEach, describe, expect, it, vi } from 'vitest';

const net = vi.hoisted(() => ({ getPage: vi.fn(), sendBody: vi.fn() }));
vi.mock('@/next/core/net/http', () => ({ getPage: net.getPage, sendBody: net.sendBody }));
vi.mock('@/next/core/net/machine', () => ({ gatewayPath: (p: string) => `/agents/api/x${p}` }));

import {
  profileSidebarStore,
  refreshProfileSidebar,
  resetProfileSidebar,
} from '@/next/stores/profile-sidebar';
import type { ConvId, ProfileSidebar } from '@/next/protocol/v2';

const doc = (loose: ConvId[]): ProfileSidebar => ({
  projects: [],
  favorites: { groups: [], loose },
  merged: ['m1'],
});

const answer = (value: { enabled: boolean; rev: number; doc: ProfileSidebar | null }) =>
  net.getPage.mockResolvedValueOnce({ ok: true, value });

// The cluster answered «not kept» for a moment it could not list the person's machines; the page then drew
// one machine's own favorites (an older order) and the person's again with the next answer, every half minute.
describe('the person sidebar under a cluster that answers «off» for a moment', () => {
  beforeEach(() => {
    net.getPage.mockReset();
    net.sendBody.mockReset();
    resetProfileSidebar();
  });

  it('keeps the person layout through the cycle on, off, on', async () => {
    const mine = doc(['tab-b', 'tab-a']);
    const seen: string[] = [];
    const stop = profileSidebarStore.subscribe(() => {
      const v = profileSidebarStore.get();
      seen.push(v.k === 'ready' ? v.doc.favorites.loose.join() : v.k);
    });
    answer({ enabled: true, rev: 4, doc: mine });
    await refreshProfileSidebar();
    answer({ enabled: false, rev: 0, doc: null });
    await refreshProfileSidebar();
    answer({ enabled: true, rev: 4, doc: mine });
    await refreshProfileSidebar();
    stop();
    expect(seen.every((s) => s === 'tab-b,tab-a')).toBe(true);
    const v = profileSidebarStore.get();
    expect(v.k === 'ready' && v.rev).toBe(4);
  });

  it('is off when the cluster keeps none from the start of the session', async () => {
    answer({ enabled: false, rev: 0, doc: null });
    await refreshProfileSidebar();
    expect(profileSidebarStore.get().k).toBe('off');
  });

  it('writes nothing by itself: only an edit sends the layout', async () => {
    answer({ enabled: true, rev: 1, doc: doc(['tab-a']) });
    await refreshProfileSidebar();
    answer({ enabled: false, rev: 0, doc: null });
    await refreshProfileSidebar();
    expect(net.sendBody).not.toHaveBeenCalled();
  });
});
