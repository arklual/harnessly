import { describe, expect, it } from 'vitest';
import { EDIT_MODE } from '@/next/features/image-edit/host';
import { NO_MARKS, type IMarks } from '@/next/features/image-edit/marks';
import {
  TOOL,
  canSend,
  isDrawing,
  modeOf,
  ratioOfFormat,
  type ISendState,
} from '@/next/features/image-edit/tool-rules';

const line: IMarks = { strokes: [{ color: '#fff', width: 0.01, points: [{ x: 0.1, y: 0.1 }] }], box: null };
const framed: IMarks = { strokes: [], box: { x0: 0.1, y0: 0.1, x1: 0.5, y1: 0.5 } };
const base: ISendState = { tool: null, marks: NO_MARKS, text: '', running: false };

describe('canSend', () => {
  it('needs words when no tool is chosen', () => {
    expect(canSend(base)).toBe(false);
    expect(canSend({ ...base, text: '  ' })).toBe(false);
    expect(canSend({ ...base, text: 'make it night' })).toBe(true);
  });
  it('mark and erase need a stroke, words are optional', () => {
    for (const tool of [TOOL.mark, TOOL.erase]) {
      expect(canSend({ ...base, tool })).toBe(false);
      expect(canSend({ ...base, tool, text: 'words alone do not do' })).toBe(false);
      expect(canSend({ ...base, tool, marks: line })).toBe(true);
    }
  });
  it('note needs a frame and words', () => {
    expect(canSend({ ...base, tool: TOOL.note, marks: framed })).toBe(false);
    expect(canSend({ ...base, tool: TOOL.note, text: 'x' })).toBe(false);
    expect(canSend({ ...base, tool: TOOL.note, marks: framed, text: 'x' })).toBe(true);
  });
  it('size is always ready; cut runs by itself and never sends', () => {
    expect(canSend({ ...base, tool: TOOL.size })).toBe(true);
    expect(canSend({ ...base, tool: TOOL.cut, text: 'x' })).toBe(false);
  });
  it('nothing sends while a change runs', () => {
    expect(canSend({ ...base, tool: TOOL.size, running: true })).toBe(false);
    expect(canSend({ ...base, text: 'x', running: true })).toBe(false);
  });
});

describe('tools', () => {
  it('only the drawing tools draw', () => {
    expect([TOOL.mark, TOOL.note, TOOL.erase].every(isDrawing)).toBe(true);
    expect([null, TOOL.cut, TOOL.size].some(isDrawing)).toBe(false);
  });
  it('no tool is the mode text', () => {
    expect(modeOf(null)).toBe(EDIT_MODE.text);
    expect(modeOf(TOOL.erase)).toBe(EDIT_MODE.erase);
  });
  it('a format is its shape', () => {
    expect(ratioOfFormat('16:9')).toBeCloseTo(1.777, 2);
    expect(ratioOfFormat('3:4')).toBe(0.75);
  });
});
