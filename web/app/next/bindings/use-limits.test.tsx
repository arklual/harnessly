// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useAgentLimits } from '@/next/bindings/use-limits';

const answer = (usedPct: number) => ({
  machines: [
    {
      id: 'local',
      limits: { claude: { fiveHour: { usedPct, resetsAt: 1_900_000_000 }, sevenDay: null }, codex: null },
    },
  ],
});

const respond = (body: unknown, status = 200) =>
  Promise.resolve(new Response(JSON.stringify(body), { status }));

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn());
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('useAgentLimits', () => {
  it("draws nothing before the machine answers, then the agent's windows, and asks the platform route of the machine", async () => {
    vi.mocked(fetch).mockImplementation(() => respond(answer(10)));
    const { result } = renderHook(() => useAgentLimits('m-ok', 'claude'));
    expect(result.current).toBeNull();
    await waitFor(() => expect(result.current).not.toBeNull());
    expect(result.current).toMatchObject({ state: 'ready', provider: 'claude' });
    expect(result.current?.state === 'ready' && result.current.limits?.fiveHour?.usedPct).toBe(10);
    expect(String(vi.mocked(fetch).mock.calls[0][0])).toMatch(/\/machines\?machine=m-ok$/);
  });

  it('reports an agent without limits as ready with none, not as a failure', async () => {
    vi.mocked(fetch).mockImplementation(() => respond(answer(10)));
    const { result } = renderHook(() => useAgentLimits('m-codex', 'codex'));
    await waitFor(() => expect(result.current).toEqual({ state: 'ready', provider: 'codex', limits: null }));
  });

  it('says it failed for a machine that never answered, and keeps the last good answer through a stumble', async () => {
    vi.mocked(fetch).mockImplementation(() => respond({ error: 'down' }, 500));
    const bad = renderHook(() => useAgentLimits('m-bad', 'claude'));
    await waitFor(() => expect(bad.result.current).toMatchObject({ state: 'failed', provider: 'claude' }));

    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(fetch).mockImplementation(() => respond(answer(20)));
    const good = renderHook(() => useAgentLimits('m-good', 'claude'));
    await waitFor(() => expect(good.result.current?.state).toBe('ready'));
    vi.mocked(fetch).mockImplementation(() => respond({ error: 'down' }, 500));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(61_000);
    });
    expect(good.result.current?.state).toBe('ready');
    vi.useRealTimers();
  });
});
