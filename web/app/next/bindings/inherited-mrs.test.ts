import { beforeAll, describe, expect, it } from 'vitest';
import { inheritedMrs } from '@/next/bindings/inherited-mrs';
import type { MadeItem } from '@/next/protocol/v2';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));

const item = (over: Partial<MadeItem>): MadeItem => ({
  kind: 'mr',
  ref: 'https://gitlab.example.com/acme/web/-/merge_requests/3',
  title: 'Draw the chart',
  op: 'created',
  at: 1,
  cover: null,
  designKind: null,
  ...over,
});

describe('the merge requests a chat inherits', () => {
  it('takes only merge requests its subchats and subagents made', () => {
    const list = inheritedMrs([
      item({ from: { kind: 'subchat', id: 'tab-1', title: 'Chart' } }),
      item({ ref: 'https://gitlab.example.com/acme/web/-/merge_requests/4' }),
      item({ kind: 'doc', ref: '/notes.md', from: { kind: 'subagent', id: 'a1', title: '' } }),
    ]);
    expect(list).toEqual([
      {
        output: {
          kind: 'mr',
          op: 'created',
          ref: 'https://gitlab.example.com/acme/web/-/merge_requests/3',
          title: 'Draw the chart',
        },
        from: 'subchat «Chart»',
      },
    ]);
  });
});
