// Runs the previous runtime's Claude transcript parser (session-parser) on the transcripts of ../ and on files built
// from them, and writes what each call returned to ../golden.json. The Go test makes the same calls on the same bytes
// and must return the same JSON (sha256 of JSON.stringify; ids come from the same counter, Date.now is fixed).
//
//   OLD_RUNTIME_SRC=<checkout of the previous runtime> OLD_RUNTIME_MODULES=<its release's node_modules> TZ=UTC \
//     node --no-warnings --experimental-transform-types --import ./register.mjs gen.mjs
//
// MASS_DIR=<folder of .jsonl> MASS_CASES=<n> MASS_OUT=<file> runs random cuts, appends and reads over that folder
// instead and writes the cases there (hashes only), for the Go test's CLAUDE_TRANSCRIPT_MASS run. FULL_OUT=<file> also
// writes every result in full, to see a difference.
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const FIXED = Date.parse('2026-10-18T00:00:00.000Z');
const RealDate = Date;
globalThis.Date = class extends RealDate {
  constructor(...args) { if (args.length) super(...args); else super(FIXED); }
  static now() { return FIXED; }
};

const parser = await import('@/lib/session-parser');
const here = path.dirname(fileURLToPath(import.meta.url));
const FIXTURES = path.resolve(here, '..');
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'transcript-golden-'));
const casePath = path.join(tmp, 'case.jsonl');

let seed = Number(process.env.MASS_SEED ?? 20261018) >>> 0;
const rand = () => {
  seed = (seed + 0x6d2b79f5) >>> 0;
  let t = seed;
  t = Math.imul(t ^ (t >>> 15), t | 1);
  t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};
const int = (n) => Math.floor(rand() * n);
const pick = (list) => list[int(list.length)];

const json = (value) => JSON.stringify(value, (_, v) => (typeof v === 'string' ? v.toWellFormed() : v));

const lineStarts = (bytes) => {
  const out = [0];
  for (let i = 0; i < bytes.length; i++) if (bytes[i] === 10 && i + 1 < bytes.length) out.push(i + 1);
  return out;
};

// A byte inside a multi-byte character when there is one, so cuts split characters too.
const midChar = (bytes) => {
  const at = [];
  for (let i = 0; i < bytes.length; i++) if ((bytes[i] & 0xc0) === 0x80) at.push(i);
  return at.length ? pick(at) : int(bytes.length + 1);
};

const run = async (c, bytes) => {
  globalThis.__ids = 0;
  const write = (cut) => fs.writeFileSync(casePath, cut === undefined ? bytes : bytes.subarray(0, cut));
  switch (c.op) {
    case 'content': {
      const text = Buffer.from(bytes.subarray(c.from ?? 0, c.to ?? bytes.length)).toString('utf-8');
      try { return json(parser.parseJsonlContent(text)); } catch { return 'throw'; }
    }
    case 'tail':
      write(c.cut);
      return json(await parser.readTailEntries(casePath, c.max));
    case 'before':
      write(c.cut);
      return json(await parser.readEntriesBefore(casePath, c.before, c.max));
    case 'file':
      write(c.cut);
      try { return json(await parser.parseJsonlFile(casePath)); } catch { return 'throw'; }
    case 'inc': {
      const results = [];
      let offset = 0;
      let pending = '';
      for (const cut of c.steps) {
        write(cut);
        const r = await parser.parseIncremental(casePath, offset, pending);
        results.push(r);
        offset = r.newOffset;
        pending = r.pendingBuffer;
      }
      return json(results);
    }
  }
  throw new Error(`unknown op ${c.op}`);
};

const fixtureCases = (file, bytes) => {
  const size = bytes.length;
  const starts = lineStarts(bytes);
  const cases = [{ op: 'content' }, { op: 'file' }];
  for (let k = 0; k < 3; k++) cases.push({ op: 'content', to: midChar(bytes) }, { op: 'content', from: pick(starts), to: int(size + 1) });
  for (const max of [1, 2, 5, 25, 1000]) cases.push({ op: 'tail', max });
  for (let k = 0; k < 2; k++) cases.push({ op: 'tail', cut: midChar(bytes), max: pick([1, 3, 1000]) });
  const befores = [size, size + 7, pick(starts), pick(starts), int(size + 1), midChar(bytes), 1, 0];
  for (const before of befores) for (const max of [1, 1000]) cases.push({ op: 'before', before, max });
  cases.push({ op: 'inc', steps: [...starts.slice(1), size] });
  const grow = [];
  for (let k = 0; k < 6; k++) grow.push(k % 2 ? midChar(bytes) : int(size + 1));
  cases.push({ op: 'inc', steps: grow.sort((a, b) => a - b) });
  cases.push({ op: 'inc', steps: [size, Math.floor(size / 2), size] });
  cases.push({ op: 'inc', steps: [midChar(bytes), size, size] });
  cases.push({ op: 'file', cut: midChar(bytes) });
  return cases.map((c) => ({ file, ...c }));
};

const bigCases = (file, bytes) => {
  const size = bytes.length;
  const cases = [{ op: 'content' }, { op: 'file' }];
  for (const max of [1, 200, 100000]) cases.push({ op: 'tail', max });
  for (const cut of [256_000, 256_001, midChar(bytes)]) cases.push({ op: 'tail', cut, max: pick([5, 100000]) });
  for (const before of [size - 1000, 300_000, 700_000, midChar(bytes)]) cases.push({ op: 'before', before, max: pick([10, 100000]) });
  cases.push({ op: 'inc', steps: [Math.floor(size / 3), midChar(bytes), size].sort((a, b) => a - b) });
  for (const cut of [1_048_576, 1_048_577]) cases.push({ op: 'file', cut });
  return cases.map((c) => ({ file, ...c }));
};

const massCases = (files, n) => {
  const cases = [];
  for (let k = 0; k < n; k++) {
    const { name, bytes } = pick(files);
    const size = bytes.length;
    const cut = () => (rand() < 0.5 ? midChar(bytes) : int(size + 1));
    const small = size < 3_000_000;
    const op = pick(small ? ['content', 'content', 'tail', 'tail', 'before', 'before', 'inc', 'inc', 'file'] : ['tail', 'before', 'inc']);
    const c = { file: name, op };
    if (op === 'content') {
      if (rand() < 0.5) c.to = cut();
      if (rand() < 0.3) c.from = pick(lineStarts(bytes));
      if (c.from !== undefined && c.to !== undefined && c.to < c.from) c.to = size;
    } else if (op === 'tail') {
      c.max = pick([1, 3, 20, 100, 400, 100000]);
      if (rand() < 0.5) c.cut = cut();
    } else if (op === 'before') {
      c.max = pick([1, 5, 50, 500, 100000]);
      c.before = rand() < 0.5 ? pick(lineStarts(bytes)) : cut();
      if (rand() < 0.3) c.cut = cut();
    } else if (op === 'inc') {
      const steps = [];
      for (let s = 0, n = 2 + int(6); s < n; s++) steps.push(cut());
      if (rand() < 0.7) steps.sort((a, b) => a - b);
      c.steps = steps;
    } else if (rand() < 0.5) c.cut = cut();
    cases.push(c);
  }
  return cases;
};

const hash = (s) => crypto.createHash('sha256').update(s, 'utf8').digest('hex');

const execute = async (cases, bytesOf, keepJSON) => {
  const full = [];
  for (const c of cases) {
    const out = await run(c, bytesOf(c.file));
    c.hash = hash(out);
    if (keepJSON && out.length <= 1500) c.json = out;
    full.push(out);
  }
  if (process.env.FULL_OUT) fs.writeFileSync(process.env.FULL_OUT, JSON.stringify(full));
};

if (process.env.MASS_DIR) {
  const dir = path.resolve(process.env.MASS_DIR);
  const files = fs.readdirSync(dir).filter((f) => f.endsWith('.jsonl')).sort().map((name) => ({ name, bytes: fs.readFileSync(path.join(dir, name)) }));
  const byName = new Map(files.map((f) => [f.name, f.bytes]));
  const cases = massCases(files, Number(process.env.MASS_CASES ?? 3000));
  await execute(cases, (name) => byName.get(name), false);
  fs.writeFileSync(process.env.MASS_OUT, JSON.stringify({ now: FIXED, dir, cases }));
} else {
  const fixtures = fs.readdirSync(FIXTURES).filter((f) => f.endsWith('.jsonl')).sort();
  const bytes = new Map(fixtures.map((f) => [f, fs.readFileSync(path.join(FIXTURES, f))]));
  // Built from every transcript that does not make the parser throw, repeated past the chunk and tail thresholds.
  const parts = fixtures.filter((f) => !f.includes('throw'));
  const once = Buffer.concat(parts.map((f) => bytes.get(f)));
  const built = {};
  for (const [name, over] of [['built-big.jsonl', 1_300_000]]) {
    const repeat = Math.ceil(over / once.length);
    built[name] = { parts, repeat };
    bytes.set(name, Buffer.concat(Array(repeat).fill(once)));
  }
  const cases = [
    ...fixtures.flatMap((f) => fixtureCases(f, bytes.get(f))),
    ...Object.keys(built).flatMap((f) => bigCases(f, bytes.get(f))),
  ];
  await execute(cases, (name) => bytes.get(name), true);
  fs.writeFileSync(path.join(FIXTURES, 'golden.json'), `${JSON.stringify({ now: FIXED, built, cases }, null, 1)}\n`);
}
fs.rmSync(tmp, { recursive: true, force: true });
