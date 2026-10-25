import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { buildMachine, procRow } from '@/next/features/pane/machine/model';
import { procKey } from '@/next/features/pane/machine/api';
import type { MachineProc, MachineSample, MachineStats } from '@/next/protocol/v2';

beforeAll(() => pinLocale('ru'));

const GB = 1024 ** 3;
const NOW = 1_792_656_000_000;

const sample = (back: number, cpu: number): MachineSample => ({
  at: NOW - back * 120_000,
  cpu,
  mem: 0.5,
  rx: 1_000_000,
  tx: 200_000,
});

const stats = (patch: Partial<MachineStats> = {}): MachineStats => ({
  at: NOW,
  cpu: 0.38,
  cores: [0.5, 0.3, 0.4, 0.1],
  mem: { used: 8 * GB, total: 16 * GB },
  net: { rx: 1_468_006, tx: 409_600 },
  disk: {
    path: '/home/coder',
    total: 120 * GB,
    free: 59 * GB,
    parts: [
      { id: 'workspace', bytes: 34 * GB },
      { id: 'caches', bytes: 15 * GB },
      { id: 'agents', bytes: 0 },
      { id: 'other', bytes: 12 * GB },
      { id: 'free', bytes: 59 * GB },
    ],
    measuredAt: NOW - 1000,
    partial: false,
  },
  history: [6, 5, 4, 3, 2, 1, 0].map((back, i) => sample(back, 0.2 + i * 0.03)),
  procs: [],
  ...patch,
});

const proc = (patch: Partial<MachineProc> = {}): MachineProc => ({
  pid: 10,
  startTime: '77',
  name: 'node · vite',
  command: 'node vite --port 4400',
  cpu: 22,
  rss: GB,
  kind: 'dev',
  port: 4400,
  stuck: false,
  guard: false,
  ...patch,
});

describe('buildMachine', () => {
  it('shows the four tiles in the design order with the real figures', () => {
    const m = buildMachine(stats());
    expect(m.kpis.map((k) => k.label)).toEqual(['Процессор', 'Память', 'Диск', 'Сеть']);
    expect(m.kpis[0]).toMatchObject({ value: '38', unit: '%', sub: '4 ядра' });
    expect(m.kpis[1]).toMatchObject({ value: '50', sub: '8 из 16 ГБ' });
    expect(m.kpis[2]).toMatchObject({ value: '51', sub: 'свободно 59 ГБ' });
    expect(m.kpis[3].sub).toBe('↑ 400 КБ/с');
  });

  it('compares the processor with ten minutes ago, and has no change when the hour is short', () => {
    const m = buildMachine(stats());
    expect(m.kpis[0].delta).toMatchObject({ dir: 'up', tone: 'bad' });
    expect(buildMachine(stats({ history: [sample(1, 0.3), sample(0, 0.4)] })).kpis[0].delta).toBeNull();
  });

  it('says the level by the worst of the processor, the memory and the disk', () => {
    expect(buildMachine(stats()).level.tone).toBe('ok');
    expect(buildMachine(stats({ cpu: 0.6 })).level).toEqual({ tone: 'warn', text: 'Машина под нагрузкой' });
    expect(buildMachine(stats({ cpu: 0.9 })).level.tone).toBe('bad');
    expect(buildMachine(stats({ mem: { used: 15.8 * GB, total: 16 * GB } })).level.tone).toBe('bad');
  });

  it('draws one cell per core and an hour of load ending at the right edge', () => {
    const m = buildMachine(stats());
    expect(m.cores).toHaveLength(4);
    expect(m.load.plot.cols.at(-1)?.vars.x).toBe('100%');
    expect(m.load.plot.cols[0].title).toBe('−12 мин');
    expect(m.load.plot.cols.at(-1)?.title).toBe('сейчас');
    expect(m.load.plot.refs).toHaveLength(1);
    expect(m.load.plot.xs).toEqual(['−60 мин', '−45', '−30', '−15', 'сейчас']);
    expect(m.net.plot.xs).toEqual(['−60 мин', '−30', 'сейчас']);
  });

  it('leaves out the parts that are empty and sums the disk to its title', () => {
    const disk = buildMachine(stats()).disk;
    expect(disk?.title).toBe('Диск · 120 ГБ');
    expect(disk?.parts.map((p) => p.id)).toEqual(['workspace', 'caches', 'other', 'free']);
    expect(disk?.note).toBeNull();
  });

  it('says so while the folders are not counted or were counted in part', () => {
    expect(buildMachine(stats({ disk: { ...stats().disk, measuredAt: null } })).disk?.note).toContain(
      'считается',
    );
    expect(buildMachine(stats({ disk: { ...stats().disk, partial: true } })).disk?.note).toContain(
      'не до конца',
    );
  });

  it('has no disk tile or bar when the filesystem could not be read', () => {
    const m = buildMachine(stats({ disk: { ...stats().disk, total: 0, free: 0, parts: [] } }));
    expect(m.disk).toBeNull();
    expect(m.kpis.map((k) => k.label)).not.toContain('Диск');
  });
});

describe('procRow', () => {
  it('names the command, the port and a hang, and bars the share of one core up to full', () => {
    const row = procRow(proc({ stuck: true, cpu: 143 }), 2);
    expect(row.sub).toBe('node vite --port 4400 · порт 4400 · не отвечает');
    expect(row.text).toBe('143%');
    expect(row.tone).toBe('bad');
    expect(row.vars).toMatchObject({ v: 1, i: 2 });
    expect(row.mem).toBe('1 ГБ');
  });

  it('keys a process by pid and start time', () => {
    expect(procKey(proc())).toBe('10:77');
  });
});
