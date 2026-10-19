// Runs the previous runtime's Codex parser on the corpus and writes what it answered, the golden file the Go parser is
// held to.
//
//   OLD_RUNTIME_SRC=<runtime checkout> OLD_RUNTIME_MODULES=<its node_modules> \
//     node --experimental-strip-types --import ./register.mjs gen.mjs golden
//   ... gen.mjs mass <corpus dir> <out.json> <seed> <count>   (random cuts and appends over every file of a folder)
//
// Ids come from a counter restarted for every case, Date.now() is pinned, and strings are written well formed: a lone
// surrogate the parser leaves by cutting text reads as U+FFFD, which is what the Go strings hold.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { resetIds } from './stub-nanoid.mjs';

const NOW = 1_772_400_000_000;
Date.now = () => NOW;

const { CodexParser, parseCodexContent, readTailCodexEntries, readCodexEntriesBefore } = await import('@/lib/session-parser-codex');
const { readCodexTimelineSessionStats, clearCodexStatsCache } = await import('@/lib/stats/jsonl-parser-codex');

const here = path.dirname(fileURLToPath(import.meta.url));
const out = (v) => JSON.stringify(v, (_, x) => (typeof x === 'string' ? x.toWellFormed() : x));
// A long answer is kept as its digest.
const keep = (s) => (s.length <= 4096 ? s : `sha256:${createHash('sha256').update(s).digest('hex')}`);

const run = async (c, dir, work) => {
  resetIds();
  const file = path.join(dir, c.file);
  const subst = (s) => s.split(dir).join('$CORPUS').split(work).join('$WORK');
  switch (c.kind) {
    case 'content':
      try {
        return subst(out(parseCodexContent(fs.readFileSync(file, 'utf-8'))));
      } catch {
        return 'throws';
      }
    case 'tail':
      return subst(out(await readTailCodexEntries(file, c.max)));
    case 'before':
      return subst(out(await readCodexEntriesBefore(file, c.before, c.max, c.until ?? undefined)));
    case 'stats':
      clearCodexStatsCache();
      return subst(out(await readCodexTimelineSessionStats(file)));
    case 'steps': {
      const src = fs.readFileSync(file);
      const target = path.join(work, path.basename(c.file));
      fs.writeFileSync(target, src.subarray(0, c.steps[0].size));
      const parser = new CodexParser(target);
      const results = [];
      for (const s of c.steps) {
        fs.writeFileSync(target, src.subarray(0, s.size));
        if (s.op === 'tail') results.push(out(await parser.parseTail(s.max)));
        else if (s.op === 'inc') results.push(out(await parser.parseIncremental()));
        else results.push(out(parser.flushStale(NOW)));
      }
      fs.rmSync(target);
      return results.map(subst);
    }
  }
  throw new Error(`unknown case ${c.kind}`);
};

// The line starts of a file, and a few cuts inside lines and inside multi-byte characters.
const cutsOf = (buf) => {
  const starts = [0];
  for (let i = 0; i < buf.length; i++) if (buf[i] === 0x0a) starts.push(i + 1);
  const multi = [];
  for (let i = 0; i < buf.length && multi.length < 64; i++) if (buf[i] >= 0xc0) multi.push(i + 1);
  return { starts, multi };
};

const goldenCases = () => {
  const cases = [];
  const corpus = path.join(here, '..', 'corpus');
  const files = fs.readdirSync(corpus).filter((f) => f.endsWith('.jsonl')).sort();
  for (const f of files) {
    const size = fs.statSync(path.join(corpus, f)).size;
    const buf = fs.readFileSync(path.join(corpus, f));
    cases.push({ name: `content/${f}`, file: f, kind: 'content' });
    cases.push({ name: `stats/${f}`, file: f, kind: 'stats' });
    if (f.startsWith('stats-')) continue;
    for (const max of [0, 1, 3, 10, 40, 100000]) cases.push({ name: `tail/${f}/${max}`, file: f, kind: 'tail', max });
    const { starts, multi } = cutsOf(buf);
    const pick = (arr, n) => (arr.length <= n ? arr : Array.from({ length: n }, (_, i) => arr[Math.floor((i * (arr.length - 1)) / (n - 1))]));
    const befores = [...new Set([0, 1, ...pick(starts, 6), ...pick(multi, 3), Math.floor(size / 2), size - 1, size, size + 100])].filter((b) => b >= 0);
    for (const before of befores) {
      for (const max of [1, 5, 50]) {
        cases.push({ name: `before/${f}/${before}/${max}`, file: f, kind: 'before', before, max, until: null });
      }
      for (const until of [0, Math.max(0, before - 7), before + 300, size + 5]) {
        cases.push({ name: `before/${f}/${before}/5/until-${until}`, file: f, kind: 'before', before, max: 5, until });
      }
    }
    if (size > 0 && size < 100_000) {
      // The file grows line by line, then by cuts in the middle of lines and characters.
      const grow = [...starts.slice(1), size].map((s) => ({ op: 'inc', size: s }));
      cases.push({ name: `steps/${f}/lines`, file: f, kind: 'steps', steps: [{ op: 'inc', size: 0 }, ...grow, { op: 'flush', size }, { op: 'flush', size }] });
      const mids = [...new Set([...starts.slice(1).map((s) => s - 9), ...multi])].filter((s) => s > 0 && s < size).sort((a, b) => a - b);
      cases.push({ name: `steps/${f}/cuts`, file: f, kind: 'steps', steps: [...mids.map((s) => ({ op: 'inc', size: s })), { op: 'inc', size }, { op: 'flush', size }] });
      const half = starts[Math.floor(starts.length / 2)];
      cases.push({
        name: `steps/${f}/tail-then-grow`,
        file: f,
        kind: 'steps',
        steps: [{ op: 'tail', max: 5, size: half + 3 }, { op: 'inc', size: half + 3 }, { op: 'inc', size }, { op: 'inc', size: half }, { op: 'inc', size }, { op: 'tail', max: 2, size }, { op: 'flush', size }],
      });
    }
  }
  const big = fs.statSync(path.join(corpus, 'big.jsonl')).size;
  cases.push({ name: 'steps/big.jsonl/grow', file: 'big.jsonl', kind: 'steps', steps: [{ op: 'tail', max: 3, size: 600_000 }, { op: 'inc', size: 1_200_007 }, { op: 'inc', size: big - 5 }, { op: 'inc', size: big }, { op: 'flush', size: big }] });
  return cases;
};

// Seeded so a run can be repeated.
const rng = (seed) => () => {
  seed = (seed + 0x6d2b79f5) | 0;
  let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};

const massCases = (dir, seed, count) => {
  const rand = rng(seed);
  const int = (n) => Math.floor(rand() * n);
  const files = fs.readdirSync(dir).filter((f) => f.endsWith('.jsonl')).sort();
  const info = Object.fromEntries(files.map((f) => {
    const buf = fs.readFileSync(path.join(dir, f));
    return [f, { size: buf.length, ...cutsOf(buf) }];
  }));
  const offset = (f) => {
    const { size, starts, multi } = info[f];
    const r = rand();
    if (r < 0.4 && starts.length) return starts[int(starts.length)];
    if (r < 0.55 && multi.length) return multi[int(multi.length)];
    return int(size + 2);
  };
  const cases = [];
  for (let i = 0; i < count; i++) {
    const f = files[int(files.length)];
    const { size } = info[f];
    const r = rand();
    const max = [0, 1, 2, 5, 20, 50, 200, 5000][int(8)];
    if (r < 0.05) cases.push({ name: `m${i}`, file: f, kind: 'content' });
    else if (r < 0.1) cases.push({ name: `m${i}`, file: f, kind: 'stats' });
    else if (r < 0.3) cases.push({ name: `m${i}`, file: f, kind: 'tail', max });
    else if (r < 0.65) cases.push({ name: `m${i}`, file: f, kind: 'before', before: offset(f), max, until: rand() < 0.5 ? null : offset(f) });
    else {
      const steps = [];
      let cur = rand() < 0.3 ? offset(f) : 0;
      steps.push(rand() < 0.5 ? { op: 'tail', max: max || 1, size: cur } : { op: 'inc', size: cur });
      for (let k = 0, n = 2 + int(8); k < n; k++) {
        const t = rand();
        if (t < 0.1) cur = offset(f);
        else if (t < 0.6) cur = Math.min(size, cur + 1 + int(4000));
        else cur = Math.min(size, Math.max(cur, offset(f)));
        steps.push(t > 0.95 ? { op: 'flush', size: cur } : { op: 'inc', size: cur });
      }
      cases.push({ name: `m${i}`, file: f, kind: 'steps', steps });
    }
  }
  return cases;
};

const [mode, ...args] = process.argv.slice(2);
const work = fs.mkdtempSync(path.join(os.tmpdir(), 'codex-golden-'));
try {
  let dir;
  let cases;
  let target;
  if (mode === 'golden') {
    dir = path.join(here, '..', 'corpus');
    cases = goldenCases();
    target = path.join(here, '..', 'golden.json');
  } else if (mode === 'mass') {
    dir = path.resolve(args[0]);
    target = args[1];
    cases = massCases(dir, Number(args[2]), Number(args[3]));
  } else {
    throw new Error('usage: gen.mjs golden | mass <dir> <out> <seed> <count>');
  }
  for (const c of cases) {
    const r = await run(c, dir, work);
    c.result = Array.isArray(r) ? r.map(keep) : keep(r);
  }
  const doc = { now: NOW, uploads: '/home/agent/.runtime/uploads', prefix: '/agents', cases };
  fs.writeFileSync(target, mode === 'golden' ? JSON.stringify(doc, null, 1) + '\n' : JSON.stringify(doc));
  console.log(`${cases.length} cases`);
} finally {
  fs.rmSync(work, { recursive: true, force: true });
}
