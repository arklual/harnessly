import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  axis,
  bytes,
  curve,
  delta,
  heat,
  kpi,
  list,
  plot,
  rate,
  slot,
  sline,
  stack,
  tone,
} from '@/next/features/pane/machine/charts';

beforeAll(() => pinLocale('ru'));

describe('tone', () => {
  it('is ok below warn, warn from warn, bad from bad', () => {
    expect([tone(54, 55, 85), tone(55, 55, 85), tone(84, 55, 85), tone(85, 55, 85)]).toEqual([
      'ok',
      'warn',
      'warn',
      'bad',
    ]);
  });
});

describe('slot', () => {
  it('wraps the palette', () => {
    expect(slot(1)).toBe('var(--hx-ch-s1)');
    expect(slot(7)).toBe('var(--hx-ch-s1)');
  });
});

describe('delta', () => {
  it('says whether the change is good by what it means, not by its direction', () => {
    expect(delta(60, 50, ' п.п.', 'down')).toEqual({ text: '+10 п.п.', tone: 'bad', dir: 'up' });
    expect(delta(40, 50, ' п.п.', 'down')).toEqual({ text: '−10 п.п.', tone: 'good', dir: 'down' });
    expect(delta(40, 50, '', 'up')).toMatchObject({ tone: 'bad', dir: 'down' });
  });

  it('is flat when the whole points are the same', () => {
    expect(delta(50.2, 49.6, ' п.п.', 'down')).toEqual({ text: '0 п.п.', tone: 'flat', dir: 'flat' });
  });
});

describe('bytes and rate', () => {
  it('counts a kilobyte as 1024 bytes and names the unit in the page language', () => {
    expect(bytes(512)).toBe('512 Б');
    expect(bytes(1536)).toBe('2 КБ');
    expect(bytes(1.5 * 1024 ** 3)).toContain('1,5');
    expect(bytes(1.5 * 1024 ** 3)).toContain('ГБ');
    expect(rate(2 * 1024 ** 2)).toBe('2 МБ/с');
  });
});

describe('curve', () => {
  it('is a polyline for two points and a spline through every point for more', () => {
    expect(
      curve(
        [
          [0, 0],
          [10, 5],
        ],
        true,
      ),
    ).toBe('M0 0L10 5');
    expect(
      curve(
        [
          [0, 0],
          [10, 5],
          [20, 0],
        ],
        true,
      ),
    ).toMatch(/^M0 0C.* 10 5C.* 20 0$/);
    expect(curve([], true)).toBe('');
  });

  it('does not overshoot the data', () => {
    const d = curve(
      [
        [0, 10],
        [10, 0],
        [20, 0],
        [30, 10],
      ],
      true,
    );
    const ys = [...d.matchAll(/C(\S+) (\S+) (\S+) (\S+) /g)].flatMap((m) => [Number(m[2]), Number(m[4])]);
    expect(Math.min(...ys)).toBeGreaterThanOrEqual(0);
    expect(Math.max(...ys)).toBeLessThanOrEqual(10);
  });
});

describe('axis', () => {
  it('takes a round step and counts the labels from the top down', () => {
    const a = axis(0, 73, 5);
    expect(a.min).toBe(0);
    expect(a.vals[0]).toBe(a.max);
    expect(a.vals.at(-1)).toBe(0);
    expect(a.max).toBeGreaterThanOrEqual(73);
  });

  it('keeps a fixed max', () => {
    expect(axis(0, 40, 5, { max: 100 }).vals).toEqual([100, 75, 50, 25, 0]);
  });
});

describe('plot', () => {
  const T0 = 1_000_000;
  const base = {
    series: [{ name: 'CPU', values: [10, 50, 90], slot: 1, format: (v: number) => `${v}%` }],
    times: [T0 + 60_000, T0 + 120_000, T0 + 180_000],
    from: T0,
    to: T0 + 180_000,
    titles: ['a', 'b', 'c'],
    xs: ['x1', 'x2'],
    min: 0,
    max: 100,
    yTicks: 5,
    yFormat: (v: number) => `${v}%`,
  };

  it('places a point by its time: a short history does not stretch over the window', () => {
    const wide = plot({ ...base, to: T0 + 600_000, times: base.times });
    const narrow = plot(base);
    expect(narrow.cols.map((c) => c.vars.x)).toEqual(['33.3%', '66.7%', '100%']);
    expect(wide.cols[2].vars.x).toBe('30%');
  });

  it('puts the tooltip on the side with room and labels the axes', () => {
    const p = plot(base);
    expect(p.cols.map((c) => c.side)).toEqual(['r', 'l', 'l']);
    expect(p.ys).toEqual(['100%', '75%', '50%', '25%', '0%']);
    expect(p.rows).toBe(4);
    expect(p.cols[1].rows).toEqual([{ label: 'CPU', value: '50%', c: 'var(--hx-ch-s1)' }]);
  });

  it('draws a line and an area closed at the bottom', () => {
    const [layer] = plot(base).layers;
    expect(layer.lineD.startsWith('M')).toBe(true);
    expect(layer.areaD.endsWith('Z')).toBe(true);
  });

  it('keeps only the reference lines inside the axis', () => {
    const p = plot({
      ...base,
      refs: [
        { value: 85, label: 'limit' },
        { value: 400, label: 'out' },
      ],
    });
    expect(p.refs.map((r) => r.label)).toEqual(['limit']);
  });

  it('draws nothing for no points', () => {
    const p = plot({ ...base, series: [{ ...base.series[0], values: [] }], times: [], titles: [] });
    expect(p.cols).toEqual([]);
    expect(p.layers[0].lineD).toBe('');
  });
});

describe('sline', () => {
  it('ends on the last value', () => {
    const s = sline([10, 20, 30], 0, 100);
    expect(s.end.x).toBe('100%');
    expect(s.lineD.startsWith('M0 ')).toBe(true);
  });

  it('is empty for no values', () => {
    expect(sline([])).toEqual({ lineD: '', areaD: '', end: {} });
  });
});

describe('heat, list, stack', () => {
  it('heat gives a saturation 0…1 and a tip', () => {
    const [a, b] = heat(
      [0, 150],
      100,
      (i) => `Ядро ${i + 1}`,
      (v) => `${v}%`,
    );
    expect(a).toEqual({ vars: { v: 0 }, tip: 'Ядро 1 · 0%' });
    expect(b.vars.v).toBe(1);
  });

  it('list clamps the bar to the max', () => {
    const [row] = list([{ key: 'k', label: 'a', sub: '', value: 143, text: '143%', tone: '' }], 100);
    expect(row.vars.v).toBe(1);
  });

  it('stack weighs the parts by their share and greys the free one', () => {
    const { parts } = stack(
      [
        { id: 'a', label: 'A', value: 25, slot: 1 },
        { id: 'free', label: 'Free', value: 75, free: true },
      ],
      100,
      (v) => `${v}`,
    );
    expect(parts[0].vars).toMatchObject({ w: 25, c: 'var(--hx-ch-s1)' });
    expect(parts[1]).toMatchObject({ free: true, vars: { w: 75, c: 'var(--hx-fg-3)' } });
  });
});

describe('kpi', () => {
  it('colours by level when it has one, by slot otherwise, and draws a spark from two points', () => {
    const level = kpi({ label: 'a', value: '1', unit: '%', index: 0, sub: '', tone: 'bad', spark: [1, 2] });
    expect(level.vars.c).toBe('var(--hx-danger)');
    expect(level.spark).not.toBeNull();
    const plain = kpi({ label: 'a', value: '1', unit: '%', index: 2, sub: '', slot: 3, spark: [1] });
    expect(plain.vars).toEqual({ c: 'var(--hx-ch-s3)', i: 2 });
    expect(plain.spark).toBeNull();
  });
});
