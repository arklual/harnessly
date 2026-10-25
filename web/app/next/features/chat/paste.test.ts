import { describe, expect, it } from 'vitest';
import { bubbleParts } from '@/next/features/chat/pasted';
import { splitPasted } from '@/next/features/chat/user-message';

describe('splitPasted', () => {
  it('keeps a plain message whole', () => {
    expect(splitPasted('hello')).toEqual([{ key: 't0', text: 'hello', pasted: false }]);
  });

  it('takes the pasted block out of its tags, with the typed text around it', () => {
    const text = 'look\n<pasted_content id="39fe">\nline one\nline two\n</pasted_content id="39fe">\nfix it';
    expect(splitPasted(text).map(({ text: t, pasted }) => [t, pasted])).toEqual([
      ['look', false],
      ['line one\nline two', true],
      ['fix it', false],
    ]);
  });
});

describe('bubbleParts', () => {
  const wrapped = 'dictated\n<pasted_content id="1">\nlong burst\n</pasted_content id="1">';

  it('keeps the paste for text typed straight into the agent', () => {
    expect(bubbleParts(wrapped, false).map((part) => part.pasted)).toEqual([false, true]);
  });

  it("shows the page's own message as plain words even when Claude wrapped it", () => {
    const parts = bubbleParts(wrapped, true);
    expect(parts.map((part) => [part.text, part.pasted])).toEqual([
      ['dictated', false],
      ['long burst', false],
    ]);
  });
});
