// Feeds generated screens to the previous runtime's live-reply reader and generated entries to its first-screen
// summary (computeInitMeta), and writes the answers to ../pieces.json; the Go test reads the same inputs. With a count
// argument it writes that many random cases to the path in PIECES_OUT instead (a one-off mass comparison).
//
//   OLD_RUNTIME_SRC=<checkout> node --no-warnings --experimental-transform-types --import ./register.mjs pieces.mjs [count]
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const lib = path.join(process.env.OLD_RUNTIME_SRC, 'src', 'lib');
const liveFile = fs.readdirSync(lib).find((f) => f.endsWith('-live-reply.ts'));
const { readLiveReply } = await import(`@/lib/${liveFile.slice(0, -3)}`);
const { computeInitMeta } = await import('@/lib/timeline-init-meta');

let seed = 7;
const rand = () => { seed = (seed * 1103515245 + 12345) % 2147483648; return seed / 2147483648; };
const pick = (a) => a[Math.floor(rand() * a.length)];
const words = ['the', 'parser', 'reads', 'lines', 'ёжик', 'данные', 'tokens', '—', '**bold**', '`code`', 'x', 'end.'];
const sentence = (n) => Array.from({ length: n }, () => pick(words)).join(' ');
const lineKinds = [
  () => `❯ ${sentence(3)}`, () => '❯ ', () => `● ${sentence(6)}`, () => `● Bash(${sentence(2)})`, () => `● Read(src/x.go)`,
  () => `  ${sentence(8)}`, () => `  ⎿ ${sentence(3)}`, () => '', () => '   ', () => `✻ Thinking… (${Math.floor(rand() * 60)}s)`,
  () => '─'.repeat(8 + Math.floor(rand() * 40)), () => `  - ${sentence(4)}`, () => `  1. ${sentence(4)}`, () => `  ## ${sentence(2)}`,
  () => `${' '.repeat(30)}Update installed · Restart`, () => `  ${sentence(3)}${' '.repeat(10)}right side`, () => `\x1b[1m● ${sentence(4)}\x1b[0m`,
  () => `* ${sentence(2)}`, () => `·  ${sentence(2)}`, () => `  ${sentence(200)}`, () => `● ${sentence(3)}`, () => `\r  ${sentence(2)}`,
];
const screen = () => Array.from({ length: 1 + Math.floor(rand() * 30) }, () => pick(lineKinds)()).join(pick(['\n', '\r\n']));
const types = ['user-message', 'assistant-message', 'tool-call', 'tool-result', 'thinking'];
const entries = () => Array.from({ length: Math.floor(rand() * 8) }, () => {
  const e = { id: 'x', type: pick(types) };
  const r = rand();
  if (r < 0.8) e.timestamp = 1760781600000 + Math.floor(rand() * 1e7);
  else if (r < 0.9) e.timestamp = 0;
  return e;
});

const count = Number(process.argv[2] ?? 0);
const n = count || 80;
const cases = [];
for (let i = 0; i < n; i++) {
  if (i % 2 === 0) {
    const s = screen();
    cases.push({ kind: 'live', screen: s, text: readLiveReply(s) });
  } else {
    const es = entries();
    const size = Math.floor(rand() * 1e6);
    const created = rand() < 0.3 ? '2026-10-18T00:00:00.000Z' : null;
    const title = rand() < 0.3 ? sentence(2) : undefined;
    let meta;
    try { meta = computeInitMeta(es, size, created, title); } catch (err) { meta = { error: err.constructor.name }; }
    cases.push({ kind: 'meta', entries: es, fileSize: size, createdAt: created, customTitle: title ?? null, meta });
  }
}
const out = count ? process.env.PIECES_OUT : path.join(here, '..', 'pieces.json');
fs.writeFileSync(out, JSON.stringify(cases, null, count ? 0 : 1) + '\n');
