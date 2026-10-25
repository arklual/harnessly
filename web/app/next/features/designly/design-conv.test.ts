import { describe, expect, it } from 'vitest';
import {
  DESIGN_CONV,
  designChats,
  inFolder,
  pickDesignConv,
  SEEN_MAX,
  withSeen,
  type IDesignConvRow,
} from '@/next/features/designly/design-conv';

const DIR = '/lib/abc';
const row = (id: string, lastActivityAt: number, cwd = DIR): IDesignConvRow => ({ id, cwd, lastActivityAt });
const conv = (id: string) => ({ k: DESIGN_CONV.conv, id });
const NONE = { k: DESIGN_CONV.none };

describe('inFolder', () => {
  it('takes the folder and what is under it, not a sibling with the same start', () => {
    expect(inFolder('/lib/abc', DIR)).toBe(true);
    expect(inFolder('/lib/abc/', DIR)).toBe(true);
    expect(inFolder('/lib/abc/pages', DIR)).toBe(true);
    expect(inFolder('/lib/abc2', DIR)).toBe(false);
  });
});

describe('pickDesignConv', () => {
  const list = [row('tab-last', 10), row('tab-from', 20, '/elsewhere'), row('tab-busy', 99)];

  it('shows the chat the person came from, over the last one', () => {
    const bound = { id: 'tab-last', fresh: false };
    expect(pickDesignConv({ arrived: 'tab-from', bound, list, answered: true })).toEqual(conv('tab-from'));
  });

  it('shows the last chat of the design when the person did not come from one', () => {
    const bound = { id: 'tab-last', fresh: false };
    expect(pickDesignConv({ arrived: null, bound, list, answered: true })).toEqual(conv('tab-last'));
    expect(pickDesignConv({ arrived: null, bound, list: null, answered: false })).toEqual(conv('tab-last'));
  });

  it("starts a new chat when there is neither, never the folder's latest", () => {
    expect(pickDesignConv({ arrived: null, bound: null, list, answered: true })).toEqual(NONE);
  });

  it('does not move when the list is read again or another chat of the folder works', () => {
    const bound = { id: 'tab-last', fresh: false };
    const again = [row('tab-busy', 500), row('tab-new', 400), row('tab-last', 10)];
    expect(pickDesignConv({ arrived: null, bound, list: again, answered: true })).toEqual(conv('tab-last'));
  });

  it('keeps a chat made a moment ago that the list has not caught up with', () => {
    const bound = { id: 'tab-made', fresh: true };
    expect(pickDesignConv({ arrived: null, bound, list, answered: true })).toEqual(conv('tab-made'));
  });

  it('lets go of a chat only the machine itself says is gone', () => {
    const bound = { id: 'tab-gone', fresh: false };
    expect(pickDesignConv({ arrived: null, bound, list, answered: false })).toEqual(conv('tab-gone'));
    expect(pickDesignConv({ arrived: null, bound, list, answered: true })).toEqual(NONE);
  });
});

describe('designChats', () => {
  it("lists the folder's chats and the ones the design was opened from, the latest first", () => {
    const list = [
      row('tab-a', 10),
      row('tab-other', 30, '/elsewhere'),
      row('tab-from', 50, '/x'),
      row('tab-b', 20),
    ];
    const ids = designChats({ list, dir: DIR, seen: ['tab-from'], touched: [], current: null }).map(
      (c) => c.id,
    );
    expect(ids).toEqual(['tab-from', 'tab-b', 'tab-a']);
  });

  it('lists after them the chats of other folders that worked on the design, the latest first, each once', () => {
    const list = [
      row('tab-old', 5, '/home/x'),
      row('tab-folder', 10),
      row('tab-new', 40, '/home/y'),
      row('tab-never', 90, '/home/z'),
      row('tab-from', 1, '/home/w'),
    ];
    const ids = designChats({
      list,
      dir: DIR,
      seen: ['tab-from'],
      touched: ['tab-old', 'tab-new', 'tab-from', 'tab-gone'],
      current: null,
    }).map((c) => c.id);
    expect(ids).toEqual(['tab-folder', 'tab-from', 'tab-new', 'tab-old']);
  });

  it('lists a chat that worked on the design before it was opened anywhere', () => {
    const list = [row('tab-hackathon', 30, '/home/coder/workspace/acme')];
    const ids = designChats({ list, dir: DIR, seen: [], touched: ['tab-hackathon'], current: null }).map(
      (c) => c.id,
    );
    expect(ids).toEqual(['tab-hackathon']);
  });

  it('keeps the current chat in the list wherever it works', () => {
    const list = [row('tab-x', 5, '/x')];
    expect(
      designChats({ list, dir: null, seen: [], touched: [], current: 'tab-x' }).map((c) => c.id),
    ).toEqual(['tab-x']);
  });
});

describe('withSeen', () => {
  it('puts the chat first once and keeps a bounded history', () => {
    expect(withSeen(['a', 'b', 'c'], 'b')).toEqual(['b', 'a', 'c']);
    const many = Array.from({ length: SEEN_MAX }, (_, i) => `c${i}`);
    expect(withSeen(many, 'new')).toHaveLength(SEEN_MAX);
  });
});
