// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePaneIntents } from '@/next/bindings/pane-intents';
import type { IPaneIntent } from '@/next/features/pane/intents';
import { setPaneAuto } from '@/next/stores/pane-auto';
import type { MachineId } from '@/next/protocol/v2';

const M = 'm-1' as MachineId;

const intent = (id: string, auto = false): IPaneIntent => ({
  id,
  command: { kind: 'open', tab: 'design', target: 'landing', ...(auto ? { auto: true } : {}) },
  createdAt: Date.now(),
  appliedAt: null,
  appliedOn: null,
});

let served: IPaneIntent[] = [];
let posted: unknown[] = [];

beforeEach(() => {
  window.localStorage.clear();
  setPaneAuto(true);
  served = [];
  posted = [];
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
  })) as unknown as typeof window.matchMedia;
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'POST') posted.push(JSON.parse(String(init.body)));
      return new Response(JSON.stringify({ intents: served }), { status: 200 });
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('usePaneIntents', () => {
  it('applies what the agent asked once, tells the machine, and never again after a look more', async () => {
    served = [intent('a')];
    const apply = vi.fn();
    renderHook(() => usePaneIntents(M, 'tab-1', apply));
    await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
    expect(apply.mock.calls[0][0].map((x: IPaneIntent) => x.id)).toEqual(['a']);
    await waitFor(() => expect(posted).toEqual([{ tabId: 'tab-1', ids: ['a'], device: 'desktop' }]));
    const before = vi.mocked(fetch).mock.calls.length;
    act(() => window.dispatchEvent(new Event('online')));
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.length).toBeGreaterThan(before));
    expect(apply).toHaveBeenCalledTimes(1);
    // The request went to the conversation's machine.
    expect(String(vi.mocked(fetch).mock.calls[0][0])).toContain('machine=m-1');
  });

  it('a page that comes back later applies what came meanwhile', async () => {
    const apply = vi.fn();
    const { unmount } = renderHook(() => usePaneIntents(M, 'tab-2', apply));
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    unmount();
    served = [intent('later')];
    renderHook(() => usePaneIntents(M, 'tab-2', apply));
    await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
  });

  it('with the switch off, what the runtime asked by itself does nothing', async () => {
    setPaneAuto(false);
    served = [intent('auto', true)];
    const apply = vi.fn();
    renderHook(() => usePaneIntents(M, 'tab-3', apply));
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    await new Promise((r) => setTimeout(r, 20));
    expect(apply).not.toHaveBeenCalled();
    expect(JSON.parse(window.localStorage.getItem('hx:pane-intents-seen:tab-3') ?? '[]')).toEqual(['auto']);
  });

  it('asks nothing while the conversation has no Legacy tab', async () => {
    renderHook(() => usePaneIntents(M, null, vi.fn()));
    await new Promise((r) => setTimeout(r, 20));
    expect(fetch).not.toHaveBeenCalled();
  });
});
