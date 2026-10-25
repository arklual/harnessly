import { describe, expect, it } from 'vitest';
import { parseNotePos } from '@/next/features/designly/note-drag';

describe('the kept place of the edit card', () => {
  it('reads a point', () => {
    expect(parseNotePos('{"x":12,"y":40}')).toEqual({ x: 12, y: 40 });
  });

  it('takes nothing else', () => {
    expect(parseNotePos(null)).toBeNull();
    expect(parseNotePos('')).toBeNull();
    expect(parseNotePos('{')).toBeNull();
    expect(parseNotePos('null')).toBeNull();
    expect(parseNotePos('{"x":"1","y":2}')).toBeNull();
    expect(parseNotePos('{"x":1}')).toBeNull();
  });
});
