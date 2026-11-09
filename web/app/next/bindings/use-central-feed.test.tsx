// @vitest-environment jsdom
// GitHub #35. A chat whose machine the cluster names offline while the link only stalls: the cluster's copy does not
// empty what the machine drew, and when the copy does replace the feed the machine's next snapshot is a whole one.
import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const page = vi.hoisted(() => ({ getPage: vi.fn() }));
vi.mock('@/next/core/net/http', () => ({ getPage: page.getPage }));
vi.mock('@/next/core/net/machine', () => ({ gatewayPath: (p: string) => `/agents/api/x${p}` }));

import { useCentralFeed } from '@/next/bindings/use-central';
import { noteCursor, resetResumes, resumeOf } from '@/next/core/realtime/cursors';
import { setScheduler } from '@/next/core/store';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { centralStore } from '@/next/stores/central';
import { connectionStore } from '@/next/stores/connection';
import { ensureFeed, feedStore } from '@/next/stores/conversation';

const M = 'm-1' as MachineId;
const C = 'tab-35' as ConvId;
const CH = `conv:${C}` as const;
const copy = (turns: unknown[]) =>
  page.getPage.mockResolvedValue({ ok: true, value: { turns, entries: [], beforeTurnId: null } });

describe('the cluster copy of an open chat', () => {
  beforeEach(() => {
    setScheduler('sync');
    page.getPage.mockReset();
    resetResumes();
    for (const c of [...feedStore.keys()]) feedStore.delete(c);
    centralStore.set({
      enabled: true,
      off: new Set([M]),
      moved: new Set(),
      names: new Map(),
      on: new Set([M]),
    });
    connectionStore.set(M, { k: 'stale', since: 1 } as never);
  });
  afterEach(cleanup);

  it('keeps the feed the machine drew when the copy has nothing of the chat', async () => {
    const f = ensureFeed(C, M);
    feedStore.set(C, { ...f, loaded: true, rows: [{ k: 'turn', id: 'turn-1' }] as never });
    noteCursor(M, CH, { epoch: 'e', seq: 9 } as never);
    copy([]);
    renderHook(() => useCentralFeed(M, C));
    await waitFor(() => expect(page.getPage).toHaveBeenCalled());
    await Promise.resolve();
    expect(feedStore.get(C)?.rows).toHaveLength(1);
    expect(resumeOf(M, CH).cursor).not.toBeNull();
  });

  it('forgets the machine cursor once the copy replaces the feed', async () => {
    noteCursor(M, CH, { epoch: 'e', seq: 9 } as never);
    copy([{ id: 'turn-1', conversationId: C, entryIds: [], startedAt: 1 }]);
    renderHook(() => useCentralFeed(M, C));
    await waitFor(() => expect(resumeOf(M, CH).cursor).toBeNull());
    expect(feedStore.get(C)?.loaded).toBe(true);
  });
});
