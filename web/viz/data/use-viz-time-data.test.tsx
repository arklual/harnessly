// @vitest-environment jsdom
// SPDX-License-Identifier: Apache-2.0
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { DAY } from '../model/time';
import type { VizGranularity } from '../model/time';
import type { VizBucketFrame, VizTimeExtent } from '../model/types';
import type { VizTimeRequest, VizTimeSource } from './source';
import { useVizOverviewData, useVizTimeData } from './use-viz-time-data';

type Id = 'a';
type Metric = 'm';

const EXTENT: VizTimeExtent = { start: 0, end: 100 * DAY, now: 100 * DAY };
const RANGE = { start: 10 * DAY, end: 20 * DAY };

const frameOf = (granularity: VizGranularity): VizBucketFrame<Id> => ({
  granularity,
  start: 0,
  step: DAY,
  length: 100,
  values: { a: new Float64Array(100) },
});

interface Call {
  readonly request: VizTimeRequest<Metric>;
  resolve(frame: VizBucketFrame<Id>): void;
  reject(error: unknown): void;
}

/** Источник, ответы которого тест отдаёт руками. */
function makeSource(extent: VizTimeExtent = EXTENT) {
  const calls: Call[] = [];
  const load = vi.fn(
    (request: VizTimeRequest<Metric>) =>
      new Promise<VizBucketFrame<Id>>((resolve, reject) => {
        calls.push({ request, resolve, reject });
      }),
  );
  const source: VizTimeSource<Id, Metric> = { extent, load };
  return { source, load, calls };
}

interface Props {
  readonly source: VizTimeSource<Id, Metric>;
  readonly granularity: VizGranularity;
  readonly range?: { start: number; end: number };
  readonly enabled?: boolean;
}

function setup(initial: Props) {
  return renderHook(
    ({ source, granularity, range = RANGE, enabled }: Props) =>
      useVizTimeData(source, { metric: 'm', granularity, range, debounce: 0, enabled }),
    { initialProps: initial },
  );
}

async function settle(call: Call, frame: VizBucketFrame<Id>) {
  await act(async () => {
    call.resolve(frame);
  });
}

async function fail(call: Call, error: unknown) {
  await act(async () => {
    call.reject(error);
  });
}

describe('useVizTimeData', () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it('starts in loading and becomes ready with the frame', async () => {
    const { source, calls } = makeSource();
    const { result } = setup({ source, granularity: 'day' });
    expect(result.current.status).toBe('loading');
    expect(result.current.frame).toBeNull();
    expect(result.current.pending).toBe('day');
    await waitFor(() => expect(calls).toHaveLength(1));

    const frame = frameOf('day');
    await settle(calls[0], frame);
    expect(result.current.status).toBe('ready');
    expect(result.current.frame).toBe(frame);
    expect(result.current.pending).toBeNull();
    expect(result.current.error).toBeNull();
  });

  it('asks the source for the window with overscan, clipped to the extent', async () => {
    const { source, calls } = makeSource();
    setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].request.metric).toBe('m');
    expect(calls[0].request.granularity).toBe('day');
    // Окно 10…20 суток, запас в одну ширину с каждой стороны.
    expect(calls[0].request.range).toEqual({ start: 0, end: 30 * DAY });
    expect(calls[0].request.signal.aborted).toBe(false);
  });

  it('clips the request to the extent edges', async () => {
    const { source, calls } = makeSource();
    setup({ source, granularity: 'day', range: { start: 90 * DAY, end: 100 * DAY } });
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0].request.range).toEqual({ start: 80 * DAY, end: 100 * DAY });
  });

  it('keeps the previous frame while a new granularity loads, then switches', async () => {
    const { source, calls } = makeSource();
    const { result, rerender } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    const dayFrame = frameOf('day');
    await settle(calls[0], dayFrame);

    rerender({ source, granularity: 'hour' });
    expect(result.current.status).toBe('refreshing');
    expect(result.current.frame).toBe(dayFrame);
    expect(result.current.pending).toBe('hour');

    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls[1].request.granularity).toBe('hour');
    const hourFrame = frameOf('hour');
    await settle(calls[1], hourFrame);
    expect(result.current.status).toBe('ready');
    expect(result.current.frame).toBe(hourFrame);
    expect(result.current.pending).toBeNull();
  });

  it('serves a granularity it already has from cache without a request', async () => {
    const { source, calls, load } = makeSource();
    const { result, rerender } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    const dayFrame = frameOf('day');
    await settle(calls[0], dayFrame);
    rerender({ source, granularity: 'hour' });
    await waitFor(() => expect(calls).toHaveLength(2));
    await settle(calls[1], frameOf('hour'));

    rerender({ source, granularity: 'day' });
    expect(result.current.status).toBe('ready');
    expect(result.current.frame).toBe(dayFrame);
    expect(load).toHaveBeenCalledTimes(2);
  });

  it('does not re-request when the window moves inside what was already asked', async () => {
    const { source, calls, load } = makeSource();
    const { result, rerender } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    await settle(calls[0], frameOf('day'));

    rerender({ source, granularity: 'day', range: { start: 12 * DAY, end: 22 * DAY } });
    expect(result.current.status).toBe('ready');
    expect(load).toHaveBeenCalledTimes(1);
  });

  it('re-requests when the window leaves the asked period', async () => {
    const { source, calls } = makeSource();
    const { result, rerender } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    const first = frameOf('day');
    await settle(calls[0], first);

    rerender({ source, granularity: 'day', range: { start: 60 * DAY, end: 70 * DAY } });
    expect(result.current.status).toBe('refreshing');
    expect(result.current.frame).toBe(first);
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls[1].request.range).toEqual({ start: 50 * DAY, end: 80 * DAY });
  });

  it('goes to error when the first request fails', async () => {
    const { source, calls } = makeSource();
    const { result } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    const boom = new Error('boom');
    await fail(calls[0], boom);
    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe(boom);
    expect(result.current.frame).toBeNull();
    expect(result.current.pending).toBeNull();
  });

  it('goes to stale-error and keeps the frame when a refresh fails', async () => {
    const { source, calls } = makeSource();
    const { result, rerender } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    const dayFrame = frameOf('day');
    await settle(calls[0], dayFrame);

    rerender({ source, granularity: 'hour' });
    await waitFor(() => expect(calls).toHaveLength(2));
    const boom = new Error('boom');
    await fail(calls[1], boom);
    expect(result.current.status).toBe('stale-error');
    expect(result.current.frame).toBe(dayFrame);
    expect(result.current.error).toBe(boom);
    expect(result.current.pending).toBeNull();
  });

  it('retry asks the source again and recovers from an error', async () => {
    const { source, calls, load } = makeSource();
    const { result } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    await fail(calls[0], new Error('boom'));
    expect(result.current.status).toBe('error');

    act(() => result.current.retry());
    expect(result.current.status).toBe('loading');
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(load).toHaveBeenCalledTimes(2);

    const frame = frameOf('day');
    await settle(calls[1], frame);
    expect(result.current.status).toBe('ready');
    expect(result.current.frame).toBe(frame);
    expect(result.current.error).toBeNull();
  });

  it('retry refetches even when the frame is cached', async () => {
    const { source, calls } = makeSource();
    const { result } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    await settle(calls[0], frameOf('day'));

    act(() => result.current.retry());
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(result.current.status).toBe('refreshing');
  });

  it('aborts the stale request when a newer one starts and ignores its late answer', async () => {
    const { source, calls } = makeSource();
    const { result, rerender } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    const stale = calls[0];
    expect(stale.request.signal.aborted).toBe(false);

    rerender({ source, granularity: 'hour' });
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(stale.request.signal.aborted).toBe(true);
    expect(calls[1].request.signal.aborted).toBe(false);

    await settle(stale, frameOf('day'));
    expect(result.current.frame).toBeNull();
    expect(result.current.status).toBe('loading');

    const fresh = frameOf('hour');
    await settle(calls[1], fresh);
    expect(result.current.frame).toBe(fresh);
    expect(result.current.status).toBe('ready');
  });

  it('ignores an AbortError and an error from an aborted request', async () => {
    const { source, calls } = makeSource();
    const { result, rerender } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    rerender({ source, granularity: 'hour' });
    await waitFor(() => expect(calls).toHaveLength(2));

    const abort = new Error('aborted');
    abort.name = 'AbortError';
    await fail(calls[0], abort);
    expect(result.current.status).toBe('loading');
    expect(result.current.error).toBeNull();
  });

  it('treats an AbortError as silence even if the request was not aborted by the hook', async () => {
    const { source, calls } = makeSource();
    const { result } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    const abort = new Error('aborted');
    abort.name = 'AbortError';
    await fail(calls[0], abort);
    expect(result.current.status).toBe('loading');
  });

  it('aborts the request on unmount', async () => {
    const { source, calls } = makeSource();
    const { unmount } = setup({ source, granularity: 'day' });
    await waitFor(() => expect(calls).toHaveLength(1));
    unmount();
    expect(calls[0].request.signal.aborted).toBe(true);
  });

  it('aborts the old request and starts over when the source changes', async () => {
    const first = makeSource();
    const second = makeSource();
    const { result, rerender } = setup({ source: first.source, granularity: 'day' });
    await waitFor(() => expect(first.calls).toHaveLength(1));

    rerender({ source: second.source, granularity: 'day' });
    await waitFor(() => expect(second.calls).toHaveLength(1));
    expect(first.calls[0].request.signal.aborted).toBe(true);

    const frame = frameOf('day');
    await settle(second.calls[0], frame);
    expect(result.current.frame).toBe(frame);
  });

  it('does not ask anything while disabled', async () => {
    const { source, load } = makeSource();
    const { result } = setup({ source, granularity: 'day', enabled: false });
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(load).not.toHaveBeenCalled();
    expect(result.current.status).toBe('loading');
  });

  it('debounces follow-up requests but not the first one', async () => {
    vi.useFakeTimers();
    const { source, calls } = makeSource();
    const { rerender } = renderHook(
      ({ granularity }: { granularity: VizGranularity }) =>
        useVizTimeData(source, { metric: 'm', granularity, range: RANGE, debounce: 200 }),
      { initialProps: { granularity: 'day' as VizGranularity } },
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(calls).toHaveLength(1);
    await act(async () => {
      calls[0].resolve(frameOf('day'));
    });

    rerender({ granularity: 'hour' });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(199);
    });
    expect(calls).toHaveLength(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(calls).toHaveLength(2);
  });
});

describe('useVizOverviewData', () => {
  it('loads the whole history in the overview granularity with no overscan', async () => {
    const { source, calls } = makeSource();
    const { result } = renderHook(() => useVizOverviewData(source, 'm'));
    await waitFor(() => expect(calls).toHaveLength(1));
    // 100 суток истории → сутки.
    expect(calls[0].request.granularity).toBe('day');
    expect(calls[0].request.range).toEqual({ start: EXTENT.start, end: EXTENT.end });
    const frame = frameOf('day');
    await settle(calls[0], frame);
    expect(result.current.frame).toBe(frame);
    expect(result.current.status).toBe('ready');
  });
});
