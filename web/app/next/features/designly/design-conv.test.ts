import { describe, expect, it } from 'vitest';
import {
  DESIGN_CONV,
  designChats,
  pickDesignConv,
  type IDesignConvRow,
} from '@/next/features/designly/design-conv';

const row = (id: string, lastActivityAt: number): IDesignConvRow => ({ id, lastActivityAt });
const conv = (id: string) => ({ k: DESIGN_CONV.conv, id });
const NONE = { k: DESIGN_CONV.none };

describe('pickDesignConv', () => {
  const list = [row('tab-last', 10), row('tab-from', 20), row('tab-busy', 99)];

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
  const RUNNER = 'runner-2';
  const ROMA = 'dev-department';
  const on = (machineId: string, id: string, lastActivityAt: number) => ({
    ...row(id, lastActivityAt),
    machineId,
  });

  it("gathers the design's chats of both machines, the latest first", () => {
    const list = [
      on(RUNNER, 'tab-r1', 10),
      on(ROMA, 'tab-d1', 40),
      on(RUNNER, 'tab-r2', 30),
      on(ROMA, 'tab-d2', 5),
      on(ROMA, 'tab-other', 90),
    ];
    const found = [
      { machine: RUNNER, convs: ['tab-r1', 'tab-r2'] },
      { machine: ROMA, convs: ['tab-d1', 'tab-d2', 'tab-gone'] },
    ];
    const ids = designChats({ list, found, fresh: null }).map((c) => `${c.machineId}:${c.id}`);
    expect(ids).toEqual([`${ROMA}:tab-d1`, `${RUNNER}:tab-r2`, `${RUNNER}:tab-r1`, `${ROMA}:tab-d2`]);
  });

  it('takes a chat only from the machine that named it', () => {
    // The same id on another machine is another chat.
    const list = [on(RUNNER, 'tab-a', 10), on(ROMA, 'tab-a', 20)];
    const ids = designChats({ list, found: [{ machine: ROMA, convs: ['tab-a'] }], fresh: null }).map(
      (c) => c.machineId,
    );
    expect(ids).toEqual([ROMA]);
  });

  it('lists no chat the machines did not name: not the one the design was opened from, not one of its folder', () => {
    // «Макет дизайн-системы Harnessly» opened the design to borrow from it; «Безопасность коротких URL» stood
    // in its folder. Neither started from the design or edited it, so the machine names neither.
    const list = [on(ROMA, 'tab-Ox54W1', 50), on(ROMA, 'tab-7dcegrokh', 60), on(ROMA, 'tab-design', 10)];
    const ids = designChats({ list, found: [{ machine: ROMA, convs: ['tab-design'] }], fresh: null }).map(
      (c) => c.id,
    );
    expect(ids).toEqual(['tab-design']);
  });

  it('keeps a chat just started from the column before the machine names it', () => {
    const list = [on(RUNNER, 'tab-new', 5)];
    expect(designChats({ list, found: [], fresh: 'tab-new' }).map((c) => c.id)).toEqual(['tab-new']);
    expect(designChats({ list: null, found: [], fresh: 'tab-new' })).toEqual([]);
  });
});
