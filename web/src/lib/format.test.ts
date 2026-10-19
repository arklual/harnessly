import { describe, expect, it } from 'vitest';

import type { MachineLoad } from '../api/types';
import { formatBytes, formatClock, formatSpan, formatUntil, formatUptime } from './format';
import { loadPerCore, metricTone, stopOutcome } from './machine-load';
import { stripControlChars } from './terminal-text';

describe('format', () => {
  it('formats clocks and spans', () => {
    expect(formatClock(247_000)).toBe('4:07');
    expect(formatClock(3_729_000)).toBe('1:02:09');
    expect(formatSpan(12_000)).toBe('12 s');
    expect(formatSpan(7_500_000)).toBe('2 h 5 min');
  });

  it('formats sizes and times', () => {
    expect(formatBytes(665_600_000)).toBe('635 MB');
    expect(formatBytes(3.5 * 1024 ** 3)).toBe('3.5 GB');
    expect(formatUptime(123_457)).toBe('1 d 10 h');
    expect(formatUntil(1000, 1000_000)).toBe('now');
    expect(formatUntil(1060, 1000_000)).toBe('in 1 min');
  });
});

describe('machine load', () => {
  const load = {
    cpu: { pct: 19, cores: 4, load1: 13 },
    mem: { usedPct: 87.5, swapUsedBytes: 0, swapInPerSec: null },
    disk: { usedPct: 50, path: '/' },
  } as MachineLoad;

  it('reads the tones', () => {
    expect(loadPerCore(load)).toBe(3.25);
    expect(metricTone('load', load)).toBe('danger');
    expect(metricTone('ram', load)).toBe('warning');
    expect(metricTone('disk', load)).toBe('success');
    expect(metricTone('swap', load)).toBe('success');
  });

  it('explains a stop', () => {
    expect(stopOutcome(200, { stopped: true })).toEqual({ kind: 'stopped' });
    expect(stopOutcome(409, { error: 'changed' }).kind).toBe('refresh');
    expect(stopOutcome(403, { error: 'protected' })).toMatchObject({ kind: 'error' });
  });
});

describe('terminal text', () => {
  it('keeps newlines and tabs, shows ESC, drops other controls', () => {
    expect(stripControlChars('a\tb\nc\u001b[201~\u0007')).toBe('a\tb\nc␛[201~');
  });
});
