import { describe, expect, it } from 'vitest';
import { LOG_KIND, MAX_LOG, mergeLog, parseLog, type ILogEntry } from '@/next/stores/notif-log';

const event = (chatId: string, key: string, at = 1, text = key): Omit<ILogEntry, 'read'> => ({
  key,
  chatId,
  kind: LOG_KIND.done,
  icon: 'circle-check',
  title: chatId,
  text,
  at,
});

describe('mergeLog', () => {
  it('puts a new event on top, unread', () => {
    const log = mergeLog([], [event('c1', 'c1:t1')], new Map([['c1', 'c1:t1']]), new Set());
    expect(log).toEqual([{ ...event('c1', 'c1:t1'), read: false }]);
  });

  it('changes nothing for the same event seen again', () => {
    const log = mergeLog([], [event('c1', 'c1:t1')], new Map([['c1', 'c1:t1']]), new Set());
    expect(mergeLog(log, [event('c1', 'c1:t1')], new Map([['c1', 'c1:t1']]), new Set())).toBe(log);
  });

  it('reads an entry once its chat moved past it, and keeps it as history', () => {
    const log = mergeLog([], [event('c1', 'c1:t1')], new Map([['c1', 'c1:t1']]), new Set());
    const after = mergeLog(log, [], new Map([['c1', null]]), new Set());
    expect(after).toEqual([{ ...event('c1', 'c1:t1'), read: true }]);
  });

  it('leaves a chat it does not see now as it was', () => {
    const log = mergeLog([], [event('c1', 'c1:t1')], new Map([['c1', 'c1:t1']]), new Set());
    expect(mergeLog(log, [], new Map(), new Set())).toBe(log);
  });

  it('does not bring back what was taken away', () => {
    expect(mergeLog([], [event('c1', 'c1:t1')], new Map([['c1', 'c1:t1']]), new Set(['c1:t1']))).toEqual([]);
  });

  it('takes a later gist of the same event (Claude’s recap)', () => {
    const log = mergeLog([], [event('c1', 'c1:t1', 1, 'начало')], new Map([['c1', 'c1:t1']]), new Set());
    const after = mergeLog(log, [event('c1', 'c1:t1', 1, 'итог')], new Map([['c1', 'c1:t1']]), new Set());
    expect(after[0]?.text).toBe('итог');
    expect(after[0]?.read).toBe(false);
  });

  it('keeps a short list', () => {
    const events = Array.from({ length: MAX_LOG + 5 }, (_, i) => event(`c${i}`, `c${i}:t`, i));
    expect(mergeLog([], events, new Map(), new Set())).toHaveLength(MAX_LOG);
  });
});

describe('parseLog', () => {
  it('drops what is not an entry', () => {
    expect(parseLog('[{"key":1},{"oops":true}]')).toEqual([]);
    expect(parseLog('not json')).toEqual([]);
  });
});
