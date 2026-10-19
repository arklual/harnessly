// Runs the previous runtime's plugin reload decisions on generated inputs and writes what they answered to
// ts-golden.json (or to OLD_OUT with OLD_CASES=<n>, for a one-off larger run). The module takes no imports but types.
//
//   OLD_RUNTIME_SRC=<checkout> OLD_MODULE_WORD=<the word its own modules start with> \
//     node --no-warnings --experimental-strip-types gen.mjs
import fs from 'node:fs';
import path from 'node:path';

for (const k of ['OLD_RUNTIME_SRC', 'OLD_MODULE_WORD']) if (!process.env[k]) throw new Error(`${k} is required`);
const mod = await import(path.join(process.env.OLD_RUNTIME_SRC, 'src/lib', `${process.env.OLD_MODULE_WORD}-claude-reload.ts`));
const here = path.dirname(new URL(import.meta.url).pathname);

let seed = Number(process.env.OLD_SEED ?? 3071);
const rnd = () => { seed = (seed * 1103515245 + 12345) % 2147483648; return seed / 2147483648; };
const pick = (a) => a[Math.floor(rnd() * a.length)];
const numbers = ['1', '0', '-5', '1.5', '1792400400000', '1e400', '-1e400', '1e-400', '"7"', 'null', 'true', '[]', '{}', '12345678901234567890'];
const markJSON = () => {
  const r = rnd();
  if (r < 0.06) return pick(['', 'x', '[]', '[1]', 'null', '7', '"s"', '{', '{} x', ' {"plugins":3} ', '{"plugins":1,"plugins":5}']);
  const parts = [];
  for (const k of ['plugins', 'skills', 'other']) if (rnd() < 0.6) parts.push(`"${k}":${pick(numbers)}`);
  if (rnd() < 0.3) parts.reverse();
  return `{${parts.join(',')}}`;
};
const mark = () => {
  const m = {};
  for (const k of ['plugins', 'skills']) if (rnd() < 0.6) m[k] = pick([1, 2, 3, 1792400400000, 1792400400001]);
  return m;
};
const states = ['idle', 'busy', 'inactive', 'ready-for-review', 'needs-input', 'cancelled', 'unknown'];
const inputs = [null, '', ' ', '  \t', ' ', '　x', 'x', ' text '];
const n = Number(process.env.OLD_CASES ?? 400);
const cases = [];
for (let i = 0; i < n; i++) {
  const raw = markJSON();
  const applied = rnd() < 0.3 ? undefined : mark();
  const m = mark();
  const tab = { panelType: pick(['claude-code', 'codex-cli', 'terminal', null]), provider: pick(['claude', 'codex', null]), cliState: pick(states), queued: rnd() < 0.2 };
  const screen = rnd() < 0.15 ? null : { busy: rnd() < 0.2, menu: rnd() < 0.2, input: pick(inputs) };
  const result = { status: pick([200, 409, 404, 500, 400]), body: { error: pick(['not-running', 'not-found', 'busy', 'menu-open', undefined]) } };
  const kind = pick(['plugins', 'skills']);
  const outcome = pick(['applied', 'wait', 'settled', 'gone']);
  const parsed = mod.parseReloadMark(raw);
  const next = mod.applyReloadOutcome(applied, m, kind, outcome);
  cases.push({
    raw, parsed,
    applied: applied ?? null, mark: m, pending: mod.pendingReloads(m, applied),
    tab, decision: mod.reloadDecision(tab, m, applied),
    screen, screenDecision: mod.screenDecision(screen),
    result, outcome: mod.reloadOutcome(result),
    kind, applyOutcome: outcome, next: next ?? null, same: next === applied,
  });
}
fs.writeFileSync(process.env.OLD_OUT ?? path.join(here, 'ts-golden.json'), JSON.stringify(cases, null, 1) + '\n');
console.log(cases.length, 'cases');
