// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { seedLimits, useAgentLimits } from '@/next/bindings/use-limits';
import { limitsProviderOf } from '@/next/features/limits/model';

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
  // the hub lists what the machine last said of its limits; the chip draws it before the machine answers,
  // and keeps it while the machine does not answer at all.
  it('draws what the hub remembers at once, and the machine’s own answer over it', async () => {
    let answer10: (r: Response) => void = () => undefined;
    vi.mocked(fetch).mockImplementation(() => new Promise<Response>((resolve) => (answer10 = resolve)));
    seedLimits('m-hub', answer(70).machines[0].limits);
    const { result } = renderHook(() => useAgentLimits('m-hub', 'claude'));
    expect(result.current?.state === 'ready' && result.current.limits?.fiveHour?.usedPct).toBe(70);
    answer10(new Response(JSON.stringify(answer(10)), { status: 200 }));
    await waitFor(() =>
      expect(result.current?.state === 'ready' && result.current.limits?.fiveHour?.usedPct).toBe(10),
    );
  });

  it('keeps what the hub remembers when the machine does not answer', async () => {
    vi.mocked(fetch).mockImplementation(() => respond({ error: 'down' }, 502));
    seedLimits('m-silent', answer(55).machines[0].limits);
    const { result } = renderHook(() => useAgentLimits('m-silent', 'claude'));
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled());
    await act(async () => undefined);
    expect(result.current?.state === 'ready' && result.current.limits?.fiveHour?.usedPct).toBe(55);
  });

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
    await waitFor(() =>
      expect(result.current).toEqual({ state: 'ready', provider: 'codex', limits: null, spend: null }),
    );
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

describe('limitsProviderOf', () => {
  it("names the agent of a chat on the CLI's own account, and nothing for a chat on a vendor's profile", () => {
    expect(limitsProviderOf({ provider: 'claude' })).toBe('claude');
    expect(limitsProviderOf({ provider: 'codex', profile: null })).toBe('codex');
    expect(limitsProviderOf({ provider: 'claude', profile: 'kimi' })).toBeNull();
    expect(limitsProviderOf({ provider: 'codex', profile: 'xai' })).toBeNull();
    expect(limitsProviderOf(null)).toBeNull();
  });
});
