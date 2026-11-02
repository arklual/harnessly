import { describe, expect, it } from 'vitest';
import { KEY_COMMANDS } from '@/next/features/keys/commands';
import {
  commandsOfChord,
  isDefault,
  keysOf,
  reset,
  takenBy,
  withChord,
  withoutChord,
} from '@/next/features/keys/keymap';

describe('the list of shortcuts', () => {
  it('has each id once, and no shortcut twice on one screen', () => {
    expect(new Set(KEY_COMMANDS.map((c) => c.id)).size).toBe(KEY_COMMANDS.length);
    for (const scope of new Set(KEY_COMMANDS.map((c) => c.scope))) {
      const chords = KEY_COMMANDS.filter((c) => c.scope === scope).flatMap((c) => c.keys);
      expect(new Set(chords).size).toBe(chords.length);
    }
  });
});

describe("the person's shortcuts", () => {
  it("are the list's own until changed", () => {
    expect(keysOf('deck.first', {})).toEqual(['Home']);
    expect(isDefault('deck.first', {})).toBe(true);
  });

  it('replace one shortcut of an action and leave the others', () => {
    const next = withChord({}, 'deck.prev', 'KeyA', 'ArrowLeft');
    expect(keysOf('deck.prev', next)).toEqual(['KeyA', 'ArrowUp', 'PageUp']);
    expect(isDefault('deck.prev', next)).toBe(false);
  });

  it('take a shortcut from the action of the same screen that had it', () => {
    expect(takenBy('deck.first', 'KeyP', {})?.id).toBe('deck.show');
    const next = withChord({}, 'deck.first', 'KeyP', null);
    expect(keysOf('deck.show', next)).toEqual([]);
    expect(keysOf('deck.first', next)).toEqual(['Home', 'KeyP']);
  });

  it('may repeat on another screen', () => {
    const next = withChord({}, 'deck.first', 'KeyK', null);
    expect(
      commandsOfChord('KeyK', next)
        .map((c) => c.id)
        .sort(),
    ).toEqual(['deck.first', 'video.toggle']);
  });

  it('go back to the list when put back, and can be turned off', () => {
    const changed = withChord({}, 'deck.first', 'KeyA', 'Home');
    expect(withChord(changed, 'deck.first', 'Home', 'KeyA')).toEqual({});
    const off = withoutChord({}, 'deck.first', 'Home');
    expect(keysOf('deck.first', off)).toEqual([]);
    expect(reset(off, 'deck.first')).toEqual({});
  });

  it('do not take a key the page keeps', () => {
    expect(withChord({}, 'pictures.next', 'Escape', null)).toEqual({});
  });
});
