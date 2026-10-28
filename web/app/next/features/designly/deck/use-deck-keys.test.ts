// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { deckKeyAction, isTypingTarget } from '@/next/features/designly/deck/use-deck-keys';
import { readBoardKey } from '@/next/features/designly/deck/use-deck-bridge';

describe('deck keys', () => {
  it('turns slides with arrows and pages; Space only outside the editor', () => {
    for (const key of ['ArrowRight', 'ArrowDown', 'PageDown'])
      expect(deckKeyAction(key, 'edit')).toBe('next');
    for (const key of ['ArrowLeft', 'ArrowUp', 'PageUp']) expect(deckKeyAction(key, 'show')).toBe('prev');
    expect(deckKeyAction(' ', 'edit')).toBeNull();
    expect(deckKeyAction(' ', 'show')).toBe('next');
    expect(deckKeyAction(' ', 'link')).toBe('next');
  });

  it('comments in the editor and by link, in either layout', () => {
    expect(deckKeyAction('c', 'edit')).toBe('comment');
    expect(deckKeyAction('С', 'link')).toBe('comment');
    expect(deckKeyAction('c', 'show')).toBeNull();
  });

  it('goes full screen only by link', () => {
    expect(deckKeyAction('f', 'link')).toBe('full');
    expect(deckKeyAction('а', 'link')).toBe('full');
    expect(deckKeyAction('f', 'edit')).toBeNull();
  });

  it('shows from the editor and records from the editor and the show', () => {
    expect(deckKeyAction('p', 'edit')).toBe('show');
    expect(deckKeyAction('з', 'edit')).toBe('show');
    expect(deckKeyAction('p', 'show')).toBeNull();
    expect(deckKeyAction('r', 'edit')).toBe('rec');
    expect(deckKeyAction('к', 'show')).toBe('rec');
    expect(deckKeyAction('r', 'rec')).toBeNull();
  });

  it('steps back with Esc anywhere', () => {
    expect(deckKeyAction('Escape', 'cut')).toBe('escape');
    expect(deckKeyAction('Enter', 'edit')).toBeNull();
  });

  it('leaves keys typed into a field to the field', () => {
    const input = document.createElement('input');
    const area = document.createElement('textarea');
    const div = document.createElement('div');
    expect(isTypingTarget(input)).toBe(true);
    expect(isTypingTarget(area)).toBe(true);
    expect(isTypingTarget(div)).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });

  it('reads the keys the board hands over', () => {
    expect(readBoardKey({ type: 'tk:key', key: 'ArrowRight' })).toBe('ArrowRight');
    expect(readBoardKey({ type: 'tk:key', key: 'Space' })).toBe(' ');
    expect(readBoardKey({ type: 'tk:key', key: 'p' })).toBe('p');
    expect(readBoardKey({ type: 'tk:key', key: 'Enter' })).toBeNull();
    expect(readBoardKey({ type: 'tk:key', key: 'toString' })).toBeNull();
    expect(readBoardKey({ type: 'tk:other', key: 'f' })).toBeNull();
  });
});
