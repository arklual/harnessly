// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import {
  deckChordOn,
  deckHandlers,
  deckKeyOn,
  isTypingTarget,
} from '@/next/features/designly/deck/use-deck-keys';
import { readBoardKey } from '@/next/features/designly/deck/use-deck-bridge';

describe('deck keys', () => {
  it('runs a slide turn on every screen, and Space turns it everywhere but the editor', () => {
    expect(deckChordOn('next', 'Space', 'edit')).toBe(false);
    expect(deckChordOn('next', 'Space', 'show')).toBe(true);
    expect(deckChordOn('next', 'ArrowRight', 'edit')).toBe(true);
    expect(deckKeyOn('next', 'edit')).toBe(true);
  });

  it('comments in the editor and by link, goes full screen only by link', () => {
    expect(deckKeyOn('comment', 'edit')).toBe(true);
    expect(deckKeyOn('comment', 'link')).toBe(true);
    expect(deckKeyOn('comment', 'show')).toBe(false);
    expect(deckKeyOn('full', 'link')).toBe(true);
    expect(deckKeyOn('full', 'edit')).toBe(false);
  });

  it('shows from the editor and records from the editor and the show', () => {
    expect(deckKeyOn('show', 'edit')).toBe(true);
    expect(deckKeyOn('show', 'show')).toBe(false);
    expect(deckKeyOn('rec', 'edit')).toBe(true);
    expect(deckKeyOn('rec', 'show')).toBe(true);
    expect(deckKeyOn('rec', 'rec')).toBe(false);
  });

  it('hands the screen only the actions that work there, and the global step keys always', () => {
    const onKey = vi.fn();
    const handlers = deckHandlers('show', onKey);
    expect(handlers['deck.comment']).toBeUndefined();
    expect(handlers['deck.next']).toBeTypeOf('function');
    handlers['global.stepOn']?.('Alt+ArrowRight');
    expect(onKey).toHaveBeenCalledWith('next');
    expect(handlers['deck.next']?.('Space')).toBeUndefined();
    expect(deckHandlers('edit', onKey)['deck.next']?.('Space')).toBe(false);
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
