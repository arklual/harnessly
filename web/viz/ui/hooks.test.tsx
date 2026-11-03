// @vitest-environment jsdom
// SPDX-License-Identifier: Apache-2.0
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { useVizSeriesState } from './hooks';

type Id = 'a' | 'b' | 'c';
const IDS: readonly Id[] = ['a', 'b', 'c'];

const hiddenOf = (result: { current: { hidden: ReadonlySet<Id> } }) => [...result.current.hidden].sort();

function setup(ids: readonly Id[] = IDS, initialHidden: readonly Id[] = []) {
  return renderHook(({ list }: { list: readonly Id[] }) => useVizSeriesState(list, initialHidden), {
    initialProps: { list: ids },
  });
}

afterEach(cleanup);

describe('useVizSeriesState', () => {
  it('starts with everything visible and nothing in focus', () => {
    const { result } = setup();
    expect(result.current.hidden.size).toBe(0);
    expect(result.current.focus).toBeNull();
  });

  it('starts with the given series hidden', () => {
    const { result } = setup(IDS, ['b']);
    expect(hiddenOf(result)).toEqual(['b']);
  });

  describe('toggle', () => {
    it('hides and shows a series', () => {
      const { result } = setup();
      act(() => result.current.toggle('a'));
      expect(hiddenOf(result)).toEqual(['a']);
      act(() => result.current.toggle('a'));
      expect(hiddenOf(result)).toEqual([]);
    });

    it('hides several series independently', () => {
      const { result } = setup();
      act(() => result.current.toggle('a'));
      act(() => result.current.toggle('b'));
      expect(hiddenOf(result)).toEqual(['a', 'b']);
    });

    it('shows everything instead of hiding the last visible series', () => {
      const { result } = setup(IDS, ['a', 'b']);
      act(() => result.current.toggle('c'));
      expect(hiddenOf(result)).toEqual([]);
    });

    it('can reveal a hidden series while others stay hidden', () => {
      const { result } = setup(IDS, ['a', 'b']);
      act(() => result.current.toggle('a'));
      expect(hiddenOf(result)).toEqual(['b']);
    });

    it('keeps a lone series visible', () => {
      const { result } = setup(['a']);
      act(() => result.current.toggle('a'));
      expect(hiddenOf(result)).toEqual([]);
    });

    it('drops the focus when the focused series is toggled', () => {
      const { result } = setup();
      act(() => result.current.setFocus('a'));
      expect(result.current.focus).toBe('a');
      act(() => result.current.toggle('a'));
      expect(result.current.focus).toBeNull();
    });

    it('keeps the focus on another series', () => {
      const { result } = setup();
      act(() => result.current.setFocus('a'));
      act(() => result.current.toggle('b'));
      expect(result.current.focus).toBe('a');
    });
  });

  describe('solo', () => {
    it('leaves only the chosen series', () => {
      const { result } = setup();
      act(() => result.current.solo('b'));
      expect(hiddenOf(result)).toEqual(['a', 'c']);
    });

    it('a repeated solo on the same series brings everything back', () => {
      const { result } = setup();
      act(() => result.current.solo('b'));
      act(() => result.current.solo('b'));
      expect(hiddenOf(result)).toEqual([]);
    });

    it('solo on another series switches the solo', () => {
      const { result } = setup();
      act(() => result.current.solo('b'));
      act(() => result.current.solo('c'));
      expect(hiddenOf(result)).toEqual(['a', 'b']);
    });

    it('solo on a hidden series shows only it', () => {
      const { result } = setup(IDS, ['a']);
      act(() => result.current.solo('a'));
      expect(hiddenOf(result)).toEqual(['b', 'c']);
    });
  });

  describe('showAll', () => {
    it('clears the hidden set', () => {
      const { result } = setup(IDS, ['a', 'b']);
      act(() => result.current.showAll());
      expect(hiddenOf(result)).toEqual([]);
    });
  });

  describe('when the series list changes', () => {
    it('forgets hidden ids that disappeared', () => {
      const { result, rerender } = setup(IDS, ['c']);
      rerender({ list: ['a', 'b'] });
      expect(hiddenOf(result)).toEqual([]);
    });

    it('keeps hidden ids that are still there', () => {
      const { result, rerender } = setup(IDS, ['a', 'c']);
      rerender({ list: ['a', 'b'] });
      expect(hiddenOf(result)).toEqual(['a']);
    });

    it('keeps the same set instance when nothing was forgotten', () => {
      const { result, rerender } = setup(IDS, ['a']);
      const before = result.current.hidden;
      rerender({ list: ['a', 'b', 'c'] });
      expect(result.current.hidden).toBe(before);
    });

    it('applies the new list to toggle (last-visible rule)', () => {
      const { result, rerender } = setup(IDS);
      rerender({ list: ['a', 'b'] });
      act(() => result.current.toggle('a'));
      act(() => result.current.toggle('b'));
      expect(hiddenOf(result)).toEqual([]);
    });
  });

  it('returns stable callbacks while ids stay the same', () => {
    const { result, rerender } = setup();
    const { toggle, solo, showAll } = result.current;
    rerender({ list: ['a', 'b', 'c'] });
    expect(result.current.toggle).toBe(toggle);
    expect(result.current.solo).toBe(solo);
    expect(result.current.showAll).toBe(showAll);
  });
});
