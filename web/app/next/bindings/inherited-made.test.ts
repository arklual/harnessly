import { describe, expect, it } from 'vitest';
import { liftedByTurn, subchatOutputs } from '@/next/bindings/inherited-made';
import type { MadeItem } from '@/next/protocol/v2';
import { viaOf } from '@/next/features/chat/delegation';
import { viaText } from '@/next/features/chat/made';
import { pinLocale } from '@/next/i18n';

const item = (over: Partial<MadeItem>): MadeItem => ({
  kind: 'design',
  ref: 'shop',
  title: 'Shop',
  op: 'changed',
  at: 1,
  cover: null,
  designKind: null,
  ...over,
});

describe('what a delegated chat made, under its card (HRN-1451)', () => {
  it('takes only the things of that subchat, as the feed cards', () => {
    const kid = { kind: 'subchat', id: 'tab-kid', title: 'Kid' } as const;
    const list = subchatOutputs(
      [
        item({ from: kid }),
        item({
          kind: 'doc',
          ref: 'https://docs.example.com/a/notes',
          title: 'Notes',
          op: 'created',
          from: kid,
        }),
        item({
          kind: 'mr',
          ref: 'https://git.example.com/a/b/-/merge_requests/1',
          from: { ...kid, id: 'tab-other' },
        }),
        item({
          kind: 'task',
          ref: 'https://tracker.example.com/issue/AB-1',
          from: { kind: 'subagent', id: 'tab-kid', title: '' },
        }),
        item({ ref: 'own' }),
      ],
      'tab-kid',
    );
    expect(list).toEqual([
      { kind: 'design', op: 'changed', ref: 'shop', title: 'Shop' },
      { kind: 'artifact', op: 'created', ref: 'https://docs.example.com/a/notes', title: 'Notes' },
    ]);
  });
});

describe('what helpers made, in the root chat’s own turns (HRN-1633)', () => {
  const kid = { kind: 'subchat', id: 'tab-kid', title: 'Kid' } as const;
  const turns = [
    { key: 't1', startedAt: 100, endedAt: 200 },
    { key: 't2', startedAt: 300, endedAt: 400 },
  ];

  it('puts a thing in the turn that ran or had just ended when it was made', () => {
    const map = liftedByTurn(
      [
        item({ kind: 'doc', ref: 'https://docs/a', title: 'A', at: 150, from: kid }),
        item({ kind: 'mr', ref: 'https://git/mr/1', title: 'MR', at: 250, from: kid }),
        item({ kind: 'board', ref: 'https://docs/b', title: 'B', at: 999, from: kid }),
      ],
      turns,
    );
    expect(map.get('t1')?.map((o) => o.ref)).toEqual(['https://docs/a']);
    // Made between the turns: the next turn tells of it; after the last: the last one.
    expect(map.get('t2')?.map((o) => o.ref)).toEqual(['https://git/mr/1', 'https://docs/b']);
  });

  it('a running turn takes what is made now; the chat’s own things and repeats are left out', () => {
    const map = liftedByTurn(
      [
        item({ kind: 'doc', ref: 'https://docs/a', at: 310, from: kid }),
        item({ kind: 'doc', ref: 'https://docs/a', at: 520, from: kid }),
        item({ kind: 'doc', ref: 'https://docs/own', at: 330 }),
      ],
      [turns[0]!, { key: 't2', startedAt: 300, endedAt: null }],
    );
    expect(map.get('t2')?.map((o) => o.ref)).toEqual(['https://docs/a']);
    expect(map.get('t1')).toBeUndefined();
  });
});

describe('the mark of who made a lifted thing (HRN-1633)', () => {
  it('a lifted card keeps the subchat it came through; the turn’s own card has none', () => {
    pinLocale('ru');
    const kid = { kind: 'subchat', id: 'tab-kid', title: 'Ресерч коннекторов' } as const;
    const helper = { kind: 'subagent', id: 'a-1', title: '' } as const;
    const map = liftedByTurn(
      [
        item({ kind: 'doc', ref: 'https://docs/a', at: 150, from: kid }),
        item({ kind: 'mr', ref: 'https://git/mr/1', at: 160, from: helper }),
      ],
      [{ key: 't1', startedAt: 100, endedAt: null }],
    );
    const [doc, mr] = map.get('t1') ?? [];
    expect(viaOf(doc!)).toEqual(kid);
    expect(viaText(doc!)).toBe('из подчата «Ресерч коннекторов»');
    expect(viaText(mr!)).toBe('от сабагента');
    expect(viaText({ kind: 'artifact', op: 'created', ref: 'https://docs/own', title: 'Own' })).toBeNull();
  });
});
