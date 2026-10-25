import { describe, expect, it } from 'vitest';
import { qrFits, qrModules } from '@/next/features/designly/qr';

// Rows of a version 2 code of 'https://x.io/abcdefg' with mask 3, as the reference encoder of Kazuhiko Arase draws it.
const REFERENCE = [
  '#######.##......#.#######',
  '#.....#...........#.....#',
  '#.###.#.#..####...#.###.#',
  '#.###.#.##.#.###..#.###.#',
  '#.###.#.####..###.#.###.#',
  '#.....#..#..#.#...#.....#',
  '#######.#.#.#.#.#.#######',
  '.........#.#..##.........',
  '####..#.#.....#.##..###.#',
  '.#..#...##......#......#.',
  '####..#.#....#....#.#....',
  '##.....##..######.#####..',
  '##.#..####.#...#.####.###',
  '.####..#####.########...#',
  '.#.#..#.#...#.#...#.#.##.',
  '#......#.#..##.#.####...#',
  '..#...#.#.##....#########',
  '........#...##..#...#.#.#',
  '#######....##...#.#.#.###',
  '#.....#..##.#..##...#..#.',
  '#.###.#..#.###..######.#.',
  '#.###.#.##..###.###.#####',
  '#.###.#.##.###.####.#.##.',
  '#.....#.#.#......##.#.#..',
  '#######.##..#.##.#.######',
];

const rowsOf = (grid: boolean[][]) => grid.map((row) => row.map((on) => (on ? '#' : '.')).join(''));

describe('qrModules', () => {
  it('draws the same modules as the reference encoder for the same mask', () => {
    const grid = qrModules('https://x.io/abcdefg', 3);
    expect(grid && rowsOf(grid)).toEqual(REFERENCE);
  });

  it('grows with the text and gives up on what does not fit', () => {
    expect(qrModules('a')?.length).toBe(21);
    expect(qrModules('a'.repeat(18))?.length).toBe(25);
    expect(qrModules('a'.repeat(271))?.length).toBe(57);
    expect(qrModules('a'.repeat(272))).toBeNull();
    expect(qrFits('a'.repeat(271))).toBe(true);
    expect(qrFits('a'.repeat(272))).toBe(false);
  });

  it('counts the bytes of a text, not its letters', () => {
    expect(qrFits('я'.repeat(135))).toBe(true);
    expect(qrFits('я'.repeat(136))).toBe(false);
  });
});
