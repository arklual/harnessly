import { describe, expect, it } from 'vitest';
import { DOCS_MAX, docsOf } from '@/next/bindings/chat-docs';
import type { MadeItem } from '@/next/protocol/v2';

const item = (over: Partial<MadeItem>): MadeItem => ({
  kind: 'doc',
  ref: 'https://docs/a',
  title: 'A',
  op: 'created',
  at: 1,
  cover: null,
  designKind: null,
  ...over,
});

describe('«Документы» of the chat (HRN-1631)', () => {
  it('documents, boards and designs only, each once by its latest change, newest first', () => {
    const docs = docsOf([
      item({ ref: 'https://docs/a', at: 1 }),
      item({
        ref: 'https://docs/a',
        op: 'changed',
        at: 5,
        from: { kind: 'subchat', id: 'tab-k', title: 'Kid' },
      }),
      item({ kind: 'board', ref: 'https://docs/b', at: 3 }),
      item({ kind: 'design', ref: 'shop', at: 4 }),
      item({ kind: 'file', ref: '/w/report.pdf', at: 9 }),
      item({ kind: 'mr', ref: 'https://git/mr/1', at: 8 }),
    ]);
    expect(docs.map((d) => [d.kind, d.ref, d.op])).toEqual([
      ['doc', 'https://docs/a', 'changed'],
      ['design', 'shop', 'created'],
      ['board', 'https://docs/b', 'created'],
    ]);
  });

  it('keeps the newest few', () => {
    const many = Array.from({ length: DOCS_MAX + 3 }, (_, i) => item({ ref: `https://docs/${i}`, at: i }));
    expect(docsOf(many)).toHaveLength(DOCS_MAX);
    expect(docsOf(many)[0]?.ref).toBe(`https://docs/${DOCS_MAX + 2}`);
  });
});
