import { afterEach, describe, expect, it } from 'vitest';
import {
  resetMarked,
  takeMarked,
  toComposer,
  type IMarkedTaker,
} from '@/next/features/image-edit/to-composer';
import type { ConvId } from '@/next/protocol/v2';

const file = (name: string) => new File(['x'], name, { type: 'image/png' });
const CHAT = 'c-1' as ConvId;
const OTHER = 'c-2' as ConvId;

const field = (has: string[] = []) => {
  const log: string[] = [];
  const taker: IMarkedTaker = {
    onFiles: (files) => log.push(...files.map((one) => `add ${one.name}`)),
    onRemove: (id) => log.push(`remove ${id}`),
    owns: (id) => has.includes(id),
  };
  return { taker, log };
};

afterEach(resetMarked);

describe('takeMarked', () => {
  it('a field takes what is meant for it or for nobody, in order, and leaves the rest', () => {
    toComposer({ file: file('a.png') });
    toComposer({ file: file('b.png'), to: OTHER });
    toComposer({ file: file('c.png'), to: CHAT });
    const mine = field();
    expect(takeMarked(CHAT, mine.taker)).toBe(2);
    expect(mine.log).toEqual(['add a.png', 'add c.png']);
    const other = field();
    expect(takeMarked(OTHER, other.taker)).toBe(1);
    expect(other.log).toEqual(['add b.png']);
  });

  it('a marked attachment takes its place, only in the field that has it', () => {
    toComposer({ file: file('shot.png'), replace: 'att-7' });
    const stranger = field();
    expect(takeMarked(OTHER, stranger.taker)).toBe(0);
    const owner = field(['att-7']);
    expect(takeMarked(CHAT, owner.taker)).toBe(1);
    expect(owner.log).toEqual(['remove att-7', 'add shot.png']);
  });

  it('nothing is taken twice', () => {
    toComposer({ file: file('a.png') });
    expect(takeMarked(CHAT, field().taker)).toBe(1);
    expect(takeMarked(CHAT, field().taker)).toBe(0);
  });
});
