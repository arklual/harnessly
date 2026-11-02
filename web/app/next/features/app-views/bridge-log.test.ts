import { afterEach, describe, expect, it } from 'vitest';
import { callsOf, clearLog, LOG_MAX, logCall, logKey } from '@/next/features/app-views/bridge-log';
import type { IBridgeLogEntry } from '@/next/features/app-views/bridge';

const entry = (n: number): IBridgeLogEntry => ({ method: `m${n}`, permission: '', ok: true, at: n });
const KEY = logKey('a.b', 'c1');

afterEach(() => clearLog(KEY));

describe('logKey', () => {
  it('names an app in a chat, and an app with no chat', () => {
    expect(logKey('a.b', 'c1')).toBe('a.b|c1');
    expect(logKey('a.b', null)).toBe('a.b|-');
  });
});

describe('logCall', () => {
  it('keeps the calls of one app in one chat apart from the others', () => {
    logCall(KEY, entry(1));
    logCall(logKey('a.b', 'c2'), entry(2));
    expect(callsOf(KEY).map((e) => e.method)).toEqual(['m1']);
    clearLog(logKey('a.b', 'c2'));
  });

  it('keeps the latest calls only, oldest first', () => {
    for (let i = 0; i < LOG_MAX + 5; i += 1) logCall(KEY, entry(i));
    const kept = callsOf(KEY);
    expect(kept).toHaveLength(LOG_MAX);
    expect(kept[0]?.method).toBe('m5');
    expect(kept.at(-1)?.method).toBe(`m${LOG_MAX + 4}`);
  });

  it('forgets everything on clear', () => {
    logCall(KEY, entry(1));
    clearLog(KEY);
    expect(callsOf(KEY)).toEqual([]);
  });
});
