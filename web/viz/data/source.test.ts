// SPDX-License-Identifier: Apache-2.0
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { VizHourlySeries } from '../model/aggregate';
import { DAY, fixedOffsetZone, HOUR } from '../model/time';
import { createLocalSource, frameCoverage, type VizTimeRequest } from './source';

/** Московская полночь 6 октября 2026. */
const START = Date.UTC(2026, 9, 5, 21);
const EXTENT = { start: START, end: START + 2 * DAY, now: START + 2 * DAY };

type Metric = 'tickets' | 'revenue';

const hourly: Record<Metric, VizHourlySeries<'a'>> = {
  tickets: { start: START, length: 48, values: { a: Array.from({ length: 48 }, () => 1) } },
  revenue: { start: START, length: 48, values: { a: Array.from({ length: 48 }, () => 100) } },
};

function request(
  patch: Partial<VizTimeRequest<Metric>> = {},
  signal = new AbortController().signal,
): VizTimeRequest<Metric> {
  return {
    metric: 'tickets',
    granularity: 'day',
    range: { start: START, end: START + 2 * DAY },
    signal,
    ...patch,
  };
}

describe('createLocalSource', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('exposes the extent it was given', () => {
    expect(createLocalSource({ hourly, extent: EXTENT }).extent).toBe(EXTENT);
  });

  it('answers with a frame bucketized to the requested granularity', async () => {
    const source = createLocalSource({ hourly, extent: EXTENT });
    const day = await source.load(request());
    expect(day.granularity).toBe('day');
    expect(day.start).toBe(START);
    expect(day.length).toBe(2);
    expect(Array.from(day.values.a)).toEqual([24, 24]);
    const hour = await source.load(request({ granularity: 'hour' }));
    expect(hour.length).toBe(48);
    expect(hour.step).toBe(HOUR);
  });

  it('picks the series of the requested metric', async () => {
    const source = createLocalSource({ hourly, extent: EXTENT });
    const frame = await source.load(request({ metric: 'revenue' }));
    expect(Array.from(frame.values.a)).toEqual([2400, 2400]);
  });

  it('answers with the whole history regardless of the requested range', async () => {
    const source = createLocalSource({ hourly, extent: EXTENT });
    const frame = await source.load(request({ range: { start: START + DAY, end: START + DAY + 3 * HOUR } }));
    expect(frame.length).toBe(2);
  });

  it('remembers built frames per metric and granularity', async () => {
    const source = createLocalSource({ hourly, extent: EXTENT });
    const first = await source.load(request());
    expect(await source.load(request())).toBe(first);
    expect(await source.load(request({ granularity: 'hour' }))).not.toBe(first);
    expect(await source.load(request({ metric: 'revenue' }))).not.toBe(first);
  });

  it('aligns buckets with the given zone', async () => {
    const source = createLocalSource({ hourly, extent: EXTENT, zone: fixedOffsetZone(0) });
    const frame = await source.load(request());
    expect(frame.start).toBe(Date.UTC(2026, 9, 5));
  });

  describe('latency', () => {
    it('does not resolve before the delay and resolves after it', async () => {
      vi.useFakeTimers();
      const source = createLocalSource({ hourly, extent: EXTENT, latency: 300 });
      const done = vi.fn();
      const promise = source.load(request()).then(done);
      await vi.advanceTimersByTimeAsync(299);
      expect(done).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      await promise;
      expect(done).toHaveBeenCalledTimes(1);
    });

    it('accepts a function of the request', async () => {
      vi.useFakeTimers();
      const latency = vi.fn((r: VizTimeRequest<Metric>) => (r.granularity === 'hour' ? 500 : 100));
      const source = createLocalSource({ hourly, extent: EXTENT, latency });
      const fast = vi.fn();
      const slow = vi.fn();
      const a = source.load(request({ granularity: 'day' })).then(fast);
      const b = source.load(request({ granularity: 'hour' })).then(slow);
      await vi.advanceTimersByTimeAsync(100);
      await a;
      expect(fast).toHaveBeenCalledTimes(1);
      expect(slow).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(400);
      await b;
      expect(slow).toHaveBeenCalledTimes(1);
      expect(latency).toHaveBeenCalledTimes(2);
    });
  });

  describe('abort', () => {
    it('rejects with an AbortError when already aborted', async () => {
      const source = createLocalSource({ hourly, extent: EXTENT });
      const controller = new AbortController();
      controller.abort();
      await expect(source.load(request({}, controller.signal))).rejects.toMatchObject({ name: 'AbortError' });
    });

    it('rejects with an AbortError when aborted while waiting, and never resolves afterwards', async () => {
      vi.useFakeTimers();
      const source = createLocalSource({ hourly, extent: EXTENT, latency: 300 });
      const controller = new AbortController();
      const promise = source.load(request({}, controller.signal));
      const assertion = expect(promise).rejects.toMatchObject({
        name: 'AbortError',
        message: expect.any(String),
      });
      controller.abort();
      await assertion;
      expect(vi.getTimerCount()).toBe(0);
      await vi.advanceTimersByTimeAsync(1000);
    });

    it('the abort error is an Error', async () => {
      const source = createLocalSource({ hourly, extent: EXTENT });
      const controller = new AbortController();
      controller.abort();
      await expect(source.load(request({}, controller.signal))).rejects.toBeInstanceOf(Error);
    });
  });

  describe('fail', () => {
    it('rejects with a regular error when fail returns true', async () => {
      const source = createLocalSource({ hourly, extent: EXTENT, fail: () => true });
      const error = await source.load(request()).catch((e: unknown) => e);
      expect(error).toBeInstanceOf(Error);
      expect((error as Error).name).not.toBe('AbortError');
    });

    it('fails only for the requests it selects', async () => {
      const source = createLocalSource({ hourly, extent: EXTENT, fail: (r) => r.metric === 'revenue' });
      await expect(source.load(request({ metric: 'tickets' }))).resolves.toBeDefined();
      await expect(source.load(request({ metric: 'revenue' }))).rejects.toThrow();
    });

    it('fails after the latency, not before', async () => {
      vi.useFakeTimers();
      const source = createLocalSource({ hourly, extent: EXTENT, latency: 200, fail: () => true });
      const settled = vi.fn();
      const promise = source.load(request()).catch(settled);
      await vi.advanceTimersByTimeAsync(199);
      expect(settled).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      await promise;
      expect(settled).toHaveBeenCalledTimes(1);
    });
  });
});

describe('frameCoverage', () => {
  it('covers from the first bucket start to the end of the last one', async () => {
    const frame = await createLocalSource({ hourly, extent: EXTENT }).load(request());
    expect(frameCoverage(frame)).toEqual({ start: START, end: START + 2 * DAY });
  });
});
