// Generated cases of the library's pure functions: inputs drawn from a seeded generator, answers from the previous
// runtime's own code. gen.mjs calls this with GEN_RANDOM=<n>; ../units.json is the small committed set.
let seed = 20261019;
const rnd = () => {
  seed |= 0; seed = (seed + 0x6d2b79f5) | 0;
  let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};
const pick = (xs) => xs[Math.floor(rnd() * xs.length)];
const int = (a, b) => a + Math.floor(rnd() * (b - a + 1));

const PIECES = ['a', 'Z', 'ё', 'Ж', 'доска', 'Главная', 'Board', '10', '2', ' ', '  ', '\t', '\n', '\u0001', '\u001b', '\u007f', '\u0085',
  '​', '‮', '⁦', '﻿', ' ', ' ', '😀', 'é', 'é', 'ß', 'ﬁ', '/', '\\', '..', '.', '?', '#', '%', '%41',
  '%E0%A4', '&', '"', "'", '<', '>', '-', '_', '.dc.html', '.html', '.HTM', '?v=1', 'lib/page.html?id=x&v=', '{{', 'Все страницы', 'Ⅻ', '١٢'];
const str = (max = 8) => Array.from({ length: int(0, max) }, () => pick(PIECES)).join('');
const id = () => pick(['main', 'a-b', 'x1', '', 'Bad', '-x', 'a'.repeat(int(1, 70)), str(2)]);

export const units = async (n, { M }) => {
  const types = await M('types');
  const claims = await M('claims');
  const manifest = await M('manifest');
  const canvas = await M('canvas-json');
  const detect = await M('board-detect');
  const locate = await M('locate');
  const group = await M('group');
  const cont = await M('continue');
  const control = await M('control');
  const cases = [];
  const add = (fn, input, run) => {
    let output;
    try {
      output = { ok: run() };
    } catch (error) {
      output = { error: String(error.message) };
    }
    cases.push({ fn, input, output: 'error' in output ? output : { ok: output.ok ?? null } });
  };
  const json = () => {
    const v = () => pick([str(3), int(-5, 30000), 1440.5, null, true, [str(2)], { title: str(3) }]);
    return pick([
      { title: str(5), source: str(3), boards: Array.from({ length: int(0, 4) }, () => pick([{ title: v(), path: pick([str(4), 'A.dc.html', 'lib/page.html?id=x', 'a/../b.html', 'x.HTM']), w: v(), h: v() }, str(2), null])) },
      { boards: { [pick(['A.dc.html', 'B.dc.html', str(3)])]: { x: v(), y: v(), w: v(), h: v(), title: v() }, '7': {} }, order: pick([undefined, ['B.dc.html', 'A.dc.html', str(2)], 'x']), title: v() },
      str(3), null, [1, 2],
    ]);
  };
  for (let i = 0; i < n; i++) {
    const s = str(10);
    const max = int(1, 20);
    add('cleanText', { s, max }, () => claims.cleanClaimText(s, max));
    add('claimFileName', { s }, () => claims.claimFileName(s));
    add('cleanBoardKey', { s }, () => claims.cleanBoardKey(s));
    const p = pick([str(6), `${str(2)}.dc.html`, `lib/${str(2)}.html?${str(2)}`]);
    add('parseBoardPath', { s: p }, () => manifest.parseBoardPath(p));
    const ref = id();
    add('boardHref', { id: ref, path: p }, () => types.libraryBoardHref(ref, p));
    const page = pick([`/a/${ref}/p/${str(3)}`, `/a/${encodeURIComponent(ref)}/p/${encodeURIComponent(str(2))}/${str(2)}`, str(5)]);
    add('parsePagePath', { s: page }, () => types.parseLibraryPagePath(page));
    add('isLibraryId', { s: ref }, () => types.isLibraryId(ref));
    const raw = json();
    add('parseArtifactFile', { json: JSON.stringify(raw) ?? 'null' }, () => manifest.parseArtifactFile(raw));
    add('parseCanvas', { json: JSON.stringify(raw) ?? 'null' }, () => canvas.parseCanvasJson(raw));
    const src = pick([
      `lib/page.html?id=${pick(['pay', 'a-b', str(1)])}&v=' + (props.variant ?? '${pick(['Mobile', 'x y', 'D'])}')${pick(['', '&m=1', '&m=10'])}`,
      `<script data-props='${JSON.stringify({ variant: { options: [str(1), 'Desktop', 'Empty', 3] }, theme: pick([{}, null, [], 1]) }).replace(/'/g, '&#39;')}'></script>`,
      `<x data-props="${JSON.stringify({ variant: { options: ['A', str(1)] } }).replace(/"/g, '&quot;')}">`,
      `data-props="{bad"`, str(6),
    ]);
    add('innerPageBoard', { title: s, source: src }, () => detect.innerPageBoard(s, src));
    add('parseBoardTweaks', { source: src }, () => detect.parseBoardTweaks(src));
    const text = str(6);
    const source = Array.from({ length: int(1, 5) }, () => str(8)).join('\n') + pick(['', `\n  ${text}  tail`]);
    add('lineOfText', { source, text }, () => locate.lineOfText(source, text));
    const art = { title: str(5), projectPath: `/x/${str(3)}`, boards: Array.from({ length: int(0, 3) }, () => ({})) };
    add('agentMessage', { title: art.title, path: art.projectPath, text: s }, () => group.buildAgentMessage(art, s));
    add('agentBlockMessage', { title: art.title, path: art.projectPath, text: source }, () => group.buildAgentBlockMessage(art, source));
    const task = pick([undefined, '', s]);
    add('continuePrompt', { title: art.title, boards: art.boards.length, task: task ?? null }, () => cont.buildDesignContinuePrompt(art, task));
    const cmd = pick([{ board: str(3), view: pick(['phone', 'desktop', 'x']), panel: pick(['show', 'fold', 'x']), props: { [str(1)]: pick([1, 'a', true, null, {}]), z: 2 } }, null, [], { props: [] }, { board: '' }]);
    add('parseControl', { json: JSON.stringify(cmd) }, () => control.parseDesignControl(cmd));
    const names = Array.from({ length: int(2, 6) }, () => pick([str(2), 'Доска 10', 'Доска 2', 'alpha', 'Zeta', 'ёж', 'Ёж', 'еж', 'Board 2', 'Board 10', 'a-b', 'a_b', '_x']));
    add('sortRu', { names }, () => [...names].sort((a, b) => a.localeCompare(b, 'ru', { numeric: true })));
    add('sortRuPlain', { names }, () => [...names].sort((a, b) => a.localeCompare(b, 'ru')));
  }
  return cases;
};
