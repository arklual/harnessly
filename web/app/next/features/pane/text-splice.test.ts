// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { spliceText } from '@/next/features/pane/text-splice';

const pieces = (...texts: string[]) =>
  texts.map((text) => {
    const piece = document.createElement('t');
    piece.textContent = text;
    return piece;
  });
const read = (list: readonly Element[]) => list.map((piece) => piece.textContent);

describe('changing text kept in pieces', () => {
  it('keeps the untouched pieces as they are', () => {
    const list = pieces('Hello ', 'bold', ' world');
    spliceText(list, 'Hello bold there');
    expect(read(list)).toEqual(['Hello ', 'bold', ' there']);
  });

  it('types into the piece the caret is in', () => {
    const list = pieces('ab', 'cd');
    spliceText(list, 'abXcd');
    expect(read(list).join('')).toBe('abXcd');
    const inner = pieces('ab', 'cd');
    spliceText(inner, 'abcXd');
    expect(read(inner)).toEqual(['ab', 'cXd']);
  });

  it('takes out what spans several pieces', () => {
    const list = pieces('one ', 'two ', 'three');
    spliceText(list, 'one three');
    expect(read(list).join('')).toBe('one three');
    expect(read(list)[0]).toBe('one ');
  });

  it('empties and fills', () => {
    const list = pieces('ab', 'cd');
    spliceText(list, '');
    expect(read(list).join('')).toBe('');
    spliceText(list, 'new');
    expect(read(list).join('')).toBe('new');
  });

  it('does nothing to the same text', () => {
    const list = pieces('a', 'b');
    spliceText(list, 'ab');
    expect(read(list)).toEqual(['a', 'b']);
  });
});
