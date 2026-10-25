// A QR code for the address of the open design («Открыть на телефоне»): byte mode, error correction L, versions 1–10
// (up to 271 bytes — an address of the page, not a document). The modules come as rows of booleans; the page draws them.

const EC_L = 1;
/** Per version: EC codewords per block, then the groups of blocks as [blocks, data codewords in each]. */
const BLOCKS: readonly { ec: number; groups: readonly (readonly [number, number])[] }[] = [
  { ec: 7, groups: [[1, 19]] },
  { ec: 10, groups: [[1, 34]] },
  { ec: 15, groups: [[1, 55]] },
  { ec: 20, groups: [[1, 80]] },
  { ec: 26, groups: [[1, 108]] },
  { ec: 18, groups: [[2, 68]] },
  { ec: 20, groups: [[2, 78]] },
  { ec: 24, groups: [[2, 97]] },
  { ec: 30, groups: [[2, 116]] },
  {
    ec: 18,
    groups: [
      [2, 68],
      [2, 69],
    ],
  },
];
/** Centres of the alignment patterns, from version 2. */
const ALIGN: readonly (readonly number[])[] = [
  [],
  [6, 18],
  [6, 22],
  [6, 26],
  [6, 30],
  [6, 34],
  [6, 22, 38],
  [6, 24, 42],
  [6, 26, 46],
  [6, 28, 50],
];
const MAX_VERSION = BLOCKS.length;

const dataCodewords = (version: number): number =>
  BLOCKS[version - 1].groups.reduce((sum, [blocks, size]) => sum + blocks * size, 0);

/** GF(256) with the QR polynomial 0x11D. */
const EXP: number[] = [];
const LOG: number[] = new Array<number>(256).fill(0);
for (let i = 0, x = 1; i < 255; i++) {
  EXP[i] = x;
  LOG[x] = i;
  x <<= 1;
  if (x & 0x100) x ^= 0x11d;
}
const mul = (a: number, b: number) => (a === 0 || b === 0 ? 0 : EXP[(LOG[a] + LOG[b]) % 255]);

/** The generator of `degree` EC codewords: the product of (x - 2^i). */
const generator = (degree: number): number[] => {
  let poly = [1];
  for (let i = 0; i < degree; i++) {
    const next = new Array<number>(poly.length + 1).fill(0);
    poly.forEach((c, j) => {
      next[j] ^= c;
      next[j + 1] ^= mul(c, EXP[i]);
    });
    poly = next;
  }
  return poly;
};

const remainder = (data: readonly number[], degree: number): number[] => {
  const gen = generator(degree);
  const rest = new Array<number>(degree).fill(0);
  for (const byte of data) {
    const factor = byte ^ (rest.shift() ?? 0);
    rest.push(0);
    for (let i = 0; i < degree; i++) rest[i] ^= mul(gen[i + 1], factor);
  }
  return rest;
};

const utf8 = (text: string): number[] => Array.from(new TextEncoder().encode(text));

const bitsOf = (bytes: readonly number[], version: number): number[] => {
  const bits: number[] = [];
  const put = (value: number, length: number) => {
    for (let i = length - 1; i >= 0; i--) bits.push((value >>> i) & 1);
  };
  put(0b0100, 4);
  put(bytes.length, version < 10 ? 8 : 16);
  for (const byte of bytes) put(byte, 8);
  const capacity = dataCodewords(version) * 8;
  put(0, Math.min(4, capacity - bits.length));
  while (bits.length % 8 !== 0) bits.push(0);
  for (let pad = 0xec; bits.length < capacity; pad ^= 0xec ^ 0x11) put(pad, 8);
  return bits;
};

/** The data codewords cut into blocks, each with its EC codewords, then taken column by column. */
const interleave = (data: readonly number[], version: number): number[] => {
  const { ec: ecLength, groups } = BLOCKS[version - 1];
  const blocks: number[][] = [];
  let at = 0;
  for (const [count, size] of groups) {
    for (let i = 0; i < count; i++) {
      blocks.push(data.slice(at, at + size));
      at += size;
    }
  }
  const checks = blocks.map((block) => remainder(block, ecLength));
  const out: number[] = [];
  const longest = Math.max(...blocks.map((block) => block.length));
  for (let i = 0; i < longest; i++) for (const block of blocks) if (i < block.length) out.push(block[i]);
  for (let i = 0; i < ecLength; i++) for (const check of checks) out.push(check[i]);
  return out;
};

type TGrid = boolean[][];

const MASKS: readonly ((x: number, y: number) => boolean)[] = [
  (x, y) => (x + y) % 2 === 0,
  (_, y) => y % 2 === 0,
  (x) => x % 3 === 0,
  (x, y) => (x + y) % 3 === 0,
  (x, y) => (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0,
  (x, y) => ((x * y) % 2) + ((x * y) % 3) === 0,
  (x, y) => (((x * y) % 2) + ((x * y) % 3)) % 2 === 0,
  (x, y) => (((x + y) % 2) + ((x * y) % 3)) % 2 === 0,
];

/** The function patterns of a version: which modules are not data, and the dark ones among them. */
const frame = (version: number) => {
  const size = version * 4 + 17;
  const dark: TGrid = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const fixed: TGrid = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const set = (x: number, y: number, on: boolean) => {
    if (x < 0 || y < 0 || x >= size || y >= size) return;
    dark[y][x] = on;
    fixed[y][x] = true;
  };
  for (let i = 0; i < size; i++) {
    set(6, i, i % 2 === 0);
    set(i, 6, i % 2 === 0);
  }
  const finder = (cx: number, cy: number) => {
    for (let dy = -4; dy <= 4; dy++)
      for (let dx = -4; dx <= 4; dx++) {
        const d = Math.max(Math.abs(dx), Math.abs(dy));
        set(cx + dx, cy + dy, d !== 2 && d !== 4);
      }
  };
  finder(3, 3);
  finder(size - 4, 3);
  finder(3, size - 4);
  const centres = ALIGN[version - 1];
  for (const cy of centres)
    for (const cx of centres) {
      const onFinder =
        (cx === 6 && cy === 6) || (cx === 6 && cy === size - 7) || (cx === size - 7 && cy === 6);
      if (onFinder) continue;
      for (let dy = -2; dy <= 2; dy++)
        for (let dx = -2; dx <= 2; dx++) set(cx + dx, cy + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
    }
  // The format areas are reserved now and written for the chosen mask; the version areas are written at once.
  for (let i = 0; i < 9; i++) {
    fixed[8][i] = true;
    fixed[i][8] = true;
  }
  for (let i = 0; i < 8; i++) {
    fixed[8][size - 1 - i] = true;
    fixed[size - 1 - i][8] = true;
  }
  if (version >= 7) {
    let rem = version;
    for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
    const bits = (version << 12) | rem;
    for (let i = 0; i < 18; i++) {
      const on = ((bits >>> i) & 1) === 1;
      const a = size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      set(a, b, on);
      set(b, a, on);
    }
  }
  return { size, dark, fixed };
};

const formatBits = (mask: number): number => {
  const data = (EC_L << 3) | mask;
  let rem = data;
  for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
  return ((data << 10) | rem) ^ 0x5412;
};

const withFormat = (grid: TGrid, mask: number): TGrid => {
  const size = grid.length;
  const out = grid.map((row) => row.slice());
  const bits = formatBits(mask);
  const bit = (i: number) => ((bits >>> i) & 1) === 1;
  for (let i = 0; i <= 5; i++) out[i][8] = bit(i);
  out[7][8] = bit(6);
  out[8][8] = bit(7);
  out[8][7] = bit(8);
  for (let i = 9; i < 15; i++) out[8][14 - i] = bit(i);
  for (let i = 0; i < 8; i++) out[8][size - 1 - i] = bit(i);
  for (let i = 8; i < 15; i++) out[size - 15 + i][8] = bit(i);
  out[size - 8][8] = true;
  return out;
};

/** The spec's four penalties; the mask with the least is taken. */
const penalty = (grid: TGrid): number => {
  const size = grid.length;
  let score = 0;
  const runs = (line: (i: number) => boolean) => {
    let run = 1;
    const lengths: number[] = [];
    for (let i = 1; i < size; i++) {
      if (line(i) === line(i - 1)) run++;
      else {
        lengths.push(run);
        run = 1;
      }
    }
    lengths.push(run);
    for (const length of lengths) if (length >= 5) score += length - 2;
    // 1:1:3:1:1 with four light modules on a side: a finder that is not one.
    const colours: boolean[] = [];
    for (let i = 0; i < size; i++) colours.push(line(i));
    const text = colours.map((c) => (c ? '1' : '0')).join('');
    for (const pattern of ['00001011101', '10111010000'])
      for (let at = text.indexOf(pattern); at >= 0; at = text.indexOf(pattern, at + 1)) score += 40;
  };
  for (let y = 0; y < size; y++) runs((x) => grid[y][x]);
  for (let x = 0; x < size; x++) runs((y) => grid[y][x]);
  for (let y = 0; y < size - 1; y++)
    for (let x = 0; x < size - 1; x++)
      if (grid[y][x] === grid[y][x + 1] && grid[y][x] === grid[y + 1][x] && grid[y][x] === grid[y + 1][x + 1])
        score += 3;
  const darkCount = grid.reduce((sum, row) => sum + row.filter(Boolean).length, 0);
  const percent = (darkCount * 100) / (size * size);
  score += Math.floor(Math.abs(percent - 50) / 5) * 10;
  return score;
};

/** The data of the code drawn into the modules that are free, with a mask applied (before the format bits). */
const place = (codewords: readonly number[], version: number, mask: number): TGrid => {
  const { size, dark, fixed } = frame(version);
  const bits = codewords.flatMap((word) =>
    Array.from({ length: 8 }, (_, i) => ((word >>> (7 - i)) & 1) === 1),
  );
  let at = 0;
  // Pairs of columns from the right, up and down in turn; the vertical timing column is skipped.
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vert = 0; vert < size; vert++) {
      for (let j = 0; j < 2; j++) {
        const x = right - j;
        const up = ((right + 1) & 2) === 0;
        const y = up ? size - 1 - vert : vert;
        if (fixed[y][x]) continue;
        const bit = at < bits.length ? bits[at++] : false;
        dark[y][x] = bit !== MASKS[mask](x, y);
      }
    }
  }
  return dark;
};

/** The smallest version that holds the bytes (the mode and the count take 2 bytes, 3 from version 10). */
const versionFor = (length: number): number | null => {
  for (let version = 1; version <= MAX_VERSION; version++)
    if (length <= dataCodewords(version) - (version < 10 ? 2 : 3)) return version;
  return null;
};

/** Does the text fit a code the page can draw? */
export const qrFits = (text: string): boolean => versionFor(utf8(text).length) !== null;

/** The modules of the text's code (true: dark) with the mask of the least penalty; null when the text is too long. */
export const qrModules = (text: string, forcedMask?: number): TGrid | null => {
  const bytes = utf8(text);
  const version = versionFor(bytes.length);
  if (version === null) return null;
  const bits = bitsOf(bytes, version);
  const data = Array.from({ length: bits.length / 8 }, (_, i) =>
    bits.slice(i * 8, i * 8 + 8).reduce((word, bit) => (word << 1) | bit, 0),
  );
  const codewords = interleave(data, version);
  const masks = forcedMask === undefined ? MASKS.map((_, i) => i) : [forcedMask];
  let best: TGrid | null = null;
  let bestScore = Infinity;
  for (const mask of masks) {
    const grid = withFormat(place(codewords, version, mask), mask);
    const score = penalty(grid);
    if (score < bestScore) {
      best = grid;
      bestScore = score;
    }
  }
  return best;
};
