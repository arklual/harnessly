// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { anchorOf, rangeOf, stillHolds } from '@/next/features/chat/feed-anchor';

const feed = (html: string) => {
  document.body.innerHTML = html;
  return document.body;
};

const pick = (node: Node, from: number, endNode: Node, to: number): Range => {
  const range = document.createRange();
  range.setStart(node, from);
  range.setEnd(endNode, to);
  return range;
};

afterEach(() => {
  document.body.innerHTML = '';
});

describe('feed anchors', () => {
  it('finds the picked words again across the row’s nodes', () => {
    feed('<div class="hx-turnwrap" data-key="t1"><p>Hello <b>big</b> world</p></div>');
    const p = document.querySelector('p') as HTMLElement;
    const range = pick(p.firstChild as Text, 3, p.lastChild as Text, 3);
    const anchor = anchorOf(range);
    expect(anchor).toEqual({ key: 't1', start: 3, raw: 'lo big wo' });
    const again = rangeOf(anchor!);
    expect(again?.toString()).toBe('lo big wo');
  });

  it('looks for the words anywhere in the row once its text has moved', () => {
    feed('<div class="hx-turnwrap" data-key="t1"><p>one two three</p></div>');
    const anchor = anchorOf(
      pick(document.querySelector('p')!.firstChild!, 4, document.querySelector('p')!.firstChild!, 7),
    )!;
    document.querySelector('p')!.textContent = 'zero one two three';
    expect(rangeOf(anchor)?.toString()).toBe('two');
  });

  it('has nothing for a row that is not drawn or lost its words', () => {
    feed('<div class="hx-turnwrap" data-key="t1"><p>one two</p></div>');
    const text = document.querySelector('p')!.firstChild!;
    const anchor = anchorOf(pick(text, 0, text, 3))!;
    const range = rangeOf(anchor)!;
    expect(stillHolds(range, anchor)).toBe(true);
    document.querySelector('p')!.textContent = 'other';
    expect(stillHolds(range, anchor)).toBe(false);
    expect(rangeOf(anchor)).toBeNull();
    feed('');
    expect(rangeOf(anchor)).toBeNull();
  });

  it('cuts the words at the end of the row they start in', () => {
    feed(
      '<div class="hx-turnwrap" data-key="t1"><p>first answer</p></div><div class="hx-turnwrap" data-key="t2"><p>second</p></div>',
    );
    const [a, b] = document.querySelectorAll('p');
    expect(anchorOf(pick(a.firstChild!, 6, b.firstChild!, 3))).toEqual({
      key: 't1',
      start: 6,
      raw: 'answer',
    });
  });

  it('is null outside the feed’s rows', () => {
    feed('<p>loose words</p>');
    const text = document.querySelector('p')!.firstChild!;
    expect(anchorOf(pick(text, 0, text, 5))).toBeNull();
  });
});
