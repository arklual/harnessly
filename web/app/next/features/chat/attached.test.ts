import { describe, expect, it } from 'vitest';
import { attachedOf, readAttached } from '@/next/features/chat/attached';
import type { MessageRef } from '@/next/protocol/v2';

const block = [
  'Look at these',
  '',
  '<attached>',
  'Attached to the message:',
  '<ref kind="design" id="d1" title="Checkout &quot;v2&quot;"/> Design "Checkout" — folder …',
  '<ref kind="doc" id="https://docs.example.com/d/spec" title="Spec &lt;draft&gt; &amp; notes"/> Document …',
  '<ref kind="file" id="src/app.ts" title="app.ts"/> Project file — src/app.ts',
  '<ref kind="nope" id="x" title="x"/> not a kind',
  '</attached>',
].join('\n');

describe('readAttached', () => {
  it('takes the block off the words and reads its places back, unescaped', () => {
    const read = readAttached(block);
    expect(read.text).toBe('Look at these');
    expect(read.refs).toEqual([
      { kind: 'design', id: 'd1', title: 'Checkout "v2"', target: '' },
      {
        kind: 'doc',
        id: 'https://docs.example.com/d/spec',
        title: 'Spec <draft> & notes',
        target: 'https://docs.example.com/d/spec',
      },
      { kind: 'file', id: 'src/app.ts', title: 'app.ts', target: 'src/app.ts' },
    ]);
  });

  it('reads a message of places alone', () => {
    const read = readAttached(
      '<attached>\nAttached:\n<ref kind="task" id="ABC-1" title="ABC-1"/> Task\n</attached>',
    );
    expect(read.text).toBe('');
    expect(read.refs.map((r) => r.id)).toEqual(['ABC-1']);
  });

  it('leaves the words alone when the block is not at the end', () => {
    const text = '<attached>\n<ref kind="file" id="a" title="a"/>\n</attached>\n\nand more words';
    expect(readAttached(text)).toEqual({ text, refs: [] });
  });
});

describe('attachedOf', () => {
  const own: MessageRef[] = [
    {
      kind: 'mr',
      id: 'https://gl/x/-/merge_requests/1',
      title: 'x !1',
      target: 'https://gl/x/-/merge_requests/1',
    },
  ];

  it('shows the message own references while the runtime has not written the block', () => {
    expect(attachedOf('words', own)).toEqual({ text: 'words', refs: own });
  });

  it('prefers the block once it is there', () => {
    expect(attachedOf(block, own).refs).toHaveLength(3);
  });
});

describe('readAttached: another chat', () => {
  it('reads its chip and leaves the excerpt lines under it to the agent', () => {
    const read = readAttached(
      [
        'continue that',
        '',
        '<attached>',
        'Attached to the message:',
        '<ref kind="chat" id="tab-old" title="Old work"/> Another chat «Old work». An excerpt follows.',
        'Chat «Old work»',
        '',
        'Original request:',
        'fix the deploy',
        '</attached>',
      ].join('\n'),
    );
    expect(read.text).toBe('continue that');
    expect(read.refs).toEqual([{ kind: 'chat', id: 'tab-old', title: 'Old work', target: 'tab-old' }]);
  });
});
