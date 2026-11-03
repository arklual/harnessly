import { describe, expect, it } from 'vitest';
import type { FrameWeek } from '../../../org/api/analytics';
import {
  decodePeriod,
  encodePeriod,
  hintOf,
  initialState,
  labelOf,
  pickPreset,
  pickWeek,
  presetsOf,
  prevOf,
  queryOf,
  setEdit,
  weekCells,
} from './period';

const SPEND = [610, 640, 590, 700, 720, 760, 740, 800, 690, 720, 780, 830, 520];
const addDays = (iso: string, n: number): string => {
  const d = new Date(`${iso}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
};
// 13 weeks from Mon 3 Aug 2026; the last, 26 Oct – 1 Nov, is still going.
const WEEKS: FrameWeek[] = SPEND.map((spendUsd, i) => ({
  from: addDays('2026-08-03', i * 7),
  to: addDays('2026-08-03', i * 7 + 6),
  spendUsd,
  current: i === 12,
}));

describe('labels', () => {
  it('shows a range inside a month once and across months in full', () => {
    expect(labelOf(WEEKS, [11, 11])).toBe('19–25 окт');
    expect(labelOf(WEEKS, [8, 11])).toBe('28 сен – 25 окт');
    expect(labelOf(WEEKS, [9, 9])).toBe('5–11 окт');
    expect(labelOf(WEEKS, [12, 12])).toBe('26 окт – 1 ноя');
  });
});

describe('picking weeks', () => {
  it('takes the comparison along while it is automatic', () => {
    let s = initialState([11, 11]);
    expect(s.b).toEqual([10, 10]);
    s = pickWeek(s, 8);
    expect(s).toMatchObject({ a: [8, 8], b: [7, 7], anchor: 8 });
    s = pickWeek(s, 11);
    expect(s).toMatchObject({ a: [8, 11], b: [4, 7], anchor: null, auto: true });
  });
  it('takes the clicks in either order', () => {
    const s = pickWeek(pickWeek(initialState([11, 11]), 6), 3);
    expect(s.a).toEqual([3, 6]);
  });
  it('stops being automatic once the comparison is chosen by hand', () => {
    let s = setEdit(initialState([11, 11]), 'b');
    s = pickWeek(pickWeek(s, 2), 3);
    expect(s).toMatchObject({ b: [2, 3], a: [11, 11], auto: false });
    s = pickWeek(setEdit(s, 'a'), 9);
    expect(s.b).toEqual([2, 3]);
  });
  it('drops a half-made range when the tab changes', () => {
    const s = setEdit(pickWeek(initialState([11, 11]), 5), 'b');
    expect(s.anchor).toBeNull();
  });
  it('does not look before the first week', () => {
    expect(prevOf([0, 0])).toEqual([0, 0]);
    expect(prevOf([1, 3])).toEqual([0, 0]);
  });
  it('words the hint for what the next click does', () => {
    const s = initialState([11, 11]);
    expect(hintOf(s)).toContain('Чтобы взять несколько недель подряд');
    expect(hintOf(setEdit(s, 'b'))).toContain('По умолчанию');
    expect(hintOf(pickWeek(s, 3))).toContain('последнюю неделю отрезка');
  });
});

describe('presets', () => {
  it('builds the three choices over the last finished weeks', () => {
    const p = presetsOf(WEEKS, { id: 'c', title: 'Субагенты на Sonnet', at: '2026-10-09' });
    expect(p.map((x) => [x.a, x.b])).toEqual([
      [
        [11, 11],
        [10, 10],
      ],
      [
        [8, 11],
        [4, 7],
      ],
      [
        [10, 10],
        [9, 9],
      ],
    ]);
    expect(p[2]?.label).toBe('Неделя после «Субагенты на Sonnet» против недели до');
  });
  it('leaves out what the strip is too short for', () => {
    expect(presetsOf(WEEKS.slice(0, 4), null)).toHaveLength(1);
    expect(presetsOf([], null)).toEqual([]);
  });
  it('turns the comparison to a manual one', () => {
    const s = pickPreset(initialState([11, 11]), { id: 'p', label: '', a: [8, 11], b: [4, 7] });
    expect(s).toMatchObject({ a: [8, 11], b: [4, 7], auto: false });
  });
});

describe('the strip', () => {
  const cells = weekCells(WEEKS, initialState([11, 11]));
  it('draws the bars against the most expensive week', () => {
    expect(cells[11]?.h).toBe(1);
    expect(cells[12]?.h).toBeCloseTo(520 / 830, 5);
  });
  it('marks the period, the comparison and the unfinished week', () => {
    expect(cells.filter((c) => c.inA).map((c) => c.index)).toEqual([11]);
    expect(cells.filter((c) => c.inB).map((c) => c.index)).toEqual([10]);
    expect(cells.filter((c) => c.now).map((c) => c.index)).toEqual([12]);
  });
  it('names the month on the first week and on the first week that starts in a new one', () => {
    expect(cells.filter((c) => c.month).map((c) => [c.index, c.month])).toEqual([
      [0, 'авг'],
      [5, 'сен'],
      [9, 'окт'],
    ]);
    expect(cells[0]?.day).toBe('3–9');
  });
  it('titles a week with its money and says the last one is going', () => {
    expect(cells[11]?.title).toBe('19–25 окт · $830');
    expect(cells[12]?.title).toBe('26 окт – 1 ноя · $520 · неделя ещё идёт');
  });
  it('still draws a week without a number', () => {
    const none = weekCells([{ from: '2026-10-19', to: '2026-10-25' }], initialState([0, 0]));
    expect(none[0]?.h).toBeGreaterThan(0);
    expect(none[0]?.title).toBe('19–25 окт');
  });
});

describe('the URL', () => {
  it('round-trips a period and a hand-made comparison', () => {
    let s = pickWeek(pickWeek(initialState([11, 11]), 8), 11);
    s = pickPreset(s, { id: 'p', label: '', a: [8, 11], b: [0, 3] });
    const enc = encodePeriod(WEEKS, s);
    expect(enc).toEqual({ p: '2026-09-28_2026-10-25', c: '2026-08-03_2026-08-30' });
    expect(decodePeriod(WEEKS, enc, WEEKS[11]!)).toMatchObject({ a: [8, 11], b: [0, 3], auto: false });
  });
  it('leaves the comparison out while it is automatic', () => {
    const enc = encodePeriod(WEEKS, initialState([10, 11]));
    expect(enc.c).toBeNull();
    expect(decodePeriod(WEEKS, enc, WEEKS[11]!)).toMatchObject({ a: [10, 11], b: [8, 9], auto: true });
  });
  it('falls back to the default period for anything that is not whole weeks of the strip', () => {
    const fallback = { from: WEEKS[11]!.from, to: WEEKS[11]!.to };
    for (const p of [null, 'junk', '2026-10-20_2026-10-25', '2026-10-25_2026-10-19']) {
      expect(decodePeriod(WEEKS, { p, c: 'junk' }, fallback)).toMatchObject({ a: [11, 11], b: [10, 10] });
    }
  });
  it('asks the API for the first Monday and the last Sunday', () => {
    expect(queryOf(WEEKS, initialState([8, 11]))).toEqual({
      period: { from: '2026-09-28', to: '2026-10-25' },
      compare: { from: '2026-08-31', to: '2026-09-27' },
    });
  });
});
