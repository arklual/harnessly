import { describe, expect, it } from 'vitest';
import { chordLabel, chordOf, isChord, isCommandChord, placeOf } from '@/next/features/keys/chord';

const press = (init: Partial<Parameters<typeof chordOf>[0]>) => ({
  key: '',
  code: '',
  metaKey: false,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  ...init,
});

describe('chords', () => {
  it('writes ⌘ on a Mac and Ctrl elsewhere as the same Mod', () => {
    expect(chordOf(press({ key: 'o', code: 'KeyO', metaKey: true, shiftKey: true }), true)).toBe(
      'Mod+Shift+KeyO',
    );
    expect(chordOf(press({ key: 'o', code: 'KeyO', ctrlKey: true, shiftKey: true }), false)).toBe(
      'Mod+Shift+KeyO',
    );
  });

  it('keeps ⌃ of a Mac apart from ⌘, and drops the Windows key', () => {
    expect(chordOf(press({ key: 'k', code: 'KeyK', ctrlKey: true }), true)).toBe('Ctrl+KeyK');
    expect(chordOf(press({ key: 'k', code: 'KeyK', metaKey: true }), false)).toBeNull();
  });

  it('is the key of the place, so the Russian layout is the same key', () => {
    expect(chordOf(press({ key: 'л', code: 'KeyK' }), false)).toBe('KeyK');
    expect(placeOf('л', '')).toBe('KeyK');
    expect(placeOf('Щ', '')).toBe('KeyO');
    expect(placeOf(' ', '')).toBe('Space');
  });

  it('is nothing for a lone modifier', () => {
    expect(chordOf(press({ key: 'Shift', code: 'ShiftLeft', shiftKey: true }), false)).toBeNull();
  });

  it('tells a command shortcut from a plain key', () => {
    expect(isCommandChord('Mod+KeyK')).toBe(true);
    expect(isCommandChord('Alt+ArrowLeft')).toBe(true);
    expect(isCommandChord('Shift+Slash')).toBe(false);
    expect(isCommandChord('ArrowLeft')).toBe(false);
  });

  it('checks the written form', () => {
    expect(isChord('Mod+Shift+KeyO')).toBe(true);
    expect(isChord('Shift+Mod+KeyO')).toBe(false);
    expect(isChord('Mod+')).toBe(false);
  });

  it('labels a shortcut as the keyboard writes it', () => {
    expect(chordLabel('Mod+Shift+KeyO', 'Space', true)).toBe('⇧⌘O');
    expect(chordLabel('Mod+Shift+KeyO', 'Space', false)).toBe('Ctrl+Shift+O');
    expect(chordLabel('Alt+ArrowLeft', 'Space', true)).toBe('⌥←');
    expect(chordLabel('Space', 'Пробел', false)).toBe('Пробел');
  });
});
