import { addDays } from '../dates';
import type { ToolItem, ToolsApi } from '../api/tools';
import { later, type MockCtx } from './ctx';

const PEOPLE = ['Анна Орлова', 'Илья Седов', 'Мария Котова', 'Павел Рябов'];
const TOOLS: ReadonlyArray<[ToolItem['kind'], string, number, number]> = [
  ['mcp', 'tracker', 46, 3],
  ['mcp', 'docs', 31, 1],
  ['mcp', 'browser', 18, 6],
  ['skill', 'code-review', 24, 0],
  ['skill', 'release-notes', 9, 0],
  ['skill', 'migrate-db', 4, 2],
  ['builtin', 'bash', 120, 14],
];

const span = (from: string, to: string): string[] => {
  const out: string[] = [];
  for (let d = from; d < to && out.length < 400; d = addDays(d, 1)) out.push(d);
  return out;
};

export const toolsMock = (_ctx: MockCtx): ToolsApi => ({
  toolsList: async (_org, { from, to }) => {
    const days = span(from, to);
    const items = TOOLS.map(([kind, name, base, errs], t): ToolItem => {
      const series = days.map((_, i) =>
        Math.max(0, Math.round((base / 7) * (1 + Math.sin(i * 0.9 + t) * 0.6))),
      );
      const calls = series.reduce((a, b) => a + b, 0);
      const errors = Math.min(calls, Math.round((errs / base) * calls));
      const people = PEOPLE.slice(0, 1 + (t % PEOPLE.length)).map((name, p) => ({
        userId: `u${p}`,
        name,
        calls: Math.max(1, Math.round(calls / (p + 2))),
        errors: p === 0 ? errors : 0,
      }));
      return {
        kind,
        name,
        calls,
        errors,
        users: people.length,
        teams: 1 + (t % 2),
        lastAt: Date.now() - t * 3_600_000,
        days: series,
        people,
      };
    }).sort((a, b) => b.calls - a.calls);
    return later({ days, calls: items.reduce((a, i) => a + i.calls, 0), items });
  },
});
