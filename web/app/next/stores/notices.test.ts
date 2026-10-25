import { describe, expect, it } from 'vitest';
import {
  DEFAULT_NOTIFY,
  MAX_NOTICES,
  parseNotices,
  parseNotify,
  withNotice,
  type INotice,
} from '@/next/stores/notices';

const notice = (chatId: string, key = `${chatId}:1`): INotice => ({
  key,
  chatId,
  kind: 'done',
  icon: 'circle-check',
  title: chatId,
  text: '',
  at: 1,
});

describe('the list of notices', () => {
  it('keeps one per chat, the newest on top, and the same event once', () => {
    let list = withNotice([], notice('a'));
    list = withNotice(list, notice('b'));
    list = withNotice(list, notice('a', 'a:2'));
    expect(list.map((n) => n.key)).toEqual(['a:2', 'b:1']);
    expect(withNotice(list, notice('b'))).toBe(list);
  });

  it('keeps at most a pile of them', () => {
    let list: readonly INotice[] = [];
    for (let i = 0; i < MAX_NOTICES + 5; i += 1) list = withNotice(list, notice(`c${i}`));
    expect(list).toHaveLength(MAX_NOTICES);
    expect(list[0]?.chatId).toBe(`c${MAX_NOTICES + 4}`);
  });

  it('reads back only what is a notice', () => {
    expect(parseNotices(JSON.stringify([notice('a'), { key: 1 }, null]))).toEqual([notice('a')]);
    expect(parseNotices('{oops')).toEqual([]);
  });
});

describe('the switches', () => {
  it('are the defaults where nothing was saved, the saved ones over them', () => {
    expect(parseNotify(null)).toEqual(DEFAULT_NOTIFY);
    expect(parseNotify(JSON.stringify({ sound: false, kinds: { done: false, other: true } }))).toEqual({
      ...DEFAULT_NOTIFY,
      sound: false,
      kinds: { ...DEFAULT_NOTIFY.kinds, done: false },
    });
  });
});
