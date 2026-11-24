// @vitest-environment jsdom
import { cleanup, render, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ConnectorActions } from '@/next/bindings/connector-actions';
import { useSettingsTick } from '@/next/bindings/settings-watch';
import { setFetch } from '@/next/core/net/http';

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
});

afterEach(() => {
  cleanup();
  setFetch((url, init) => fetch(url, init));
  vi.restoreAllMocks();
  vi.useRealTimers();
});

it.each(['http/1.1', 'h2'])('uses the appropriate background requests over %s', async (protocol) => {
  vi.spyOn(performance, 'getEntriesByType').mockReturnValue([
    { nextHopProtocol: protocol } as PerformanceNavigationTiming,
  ]);
  const requests: URL[] = [];
  setFetch(async (url) => {
    const request = new URL(url, 'https://example.test');
    requests.push(request);
    if (request.searchParams.has('rev')) return new Promise<Response>(() => undefined);
    return Response.json({ rev: '1', proposals: [] });
  });

  const machine = `machine-${protocol}`;
  renderHook(() => useSettingsTick(machine));
  render(<ConnectorActions machine={machine} conversation="chat-1" />);
  await vi.advanceTimersByTimeAsync(0);

  const revisions = () => requests.map((url) => url.searchParams.get('rev'));
  if (protocol === 'http/1.1') {
    expect(revisions()).toEqual([null, null]);
    await vi.advanceTimersByTimeAsync(5000);
    expect(revisions()).toEqual([null, null, null, null]);
  } else {
    expect(revisions()).toEqual([null, null, '1', '1']);
  }
});
