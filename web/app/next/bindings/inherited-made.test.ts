import { describe, expect, it } from 'vitest';
import { subchatOutputs } from '@/next/bindings/inherited-made';
import type { MadeItem } from '@/next/protocol/v2';

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
