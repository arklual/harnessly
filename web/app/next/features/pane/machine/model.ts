// What the Machine tool draws, from what the machine says: the design's numbers (lib/hx, Главная.dc.html) with the
// real figures in them. Pure: the view gets finished tiles, charts and rows.
import { plural, t } from '@/next/i18n';
import type { DiskPartId, MachineProc, MachineStats } from '@/next/protocol/v2';
import {
  bytes,
  bytesParts,
  delta,
  heat,
  kpi,
  list,
  num,
  percent,
  plot,
  rate,
  rateParts,
  stack,
  tone,
  TONE,
  type IHeatCell,
  type IKpi,
  type IPlot,
  type TVars,
  type TTone,
} from '@/next/features/pane/machine/charts';

/** The history is a point every two minutes, a window of 30 of them. */
export const STEP_MS = 120_000;
export const POINTS = 30;
const SPAN_MS = (POINTS - 1) * STEP_MS;
/** The tiles' mini lines show the last 14 points. */
const SPARK_POINTS = 14;
/** The change in the processor's tile is against this many points back (ten minutes). */
const DELTA_BACK = 5;

/** Where a level turns from fine to loaded to overloaded, per cent. */
const CPU_LEVELS = { warn: 55, bad: 85 } as const;
const MEM_LEVELS = { warn: 85, bad: 93 } as const;
const DISK_LEVELS = { warn: 90, bad: 97 } as const;

const LEVEL_RANK = { ok: 0, warn: 1, bad: 2 } as const satisfies Record<TTone, number>;

/** The palette slot of each series, as the design gives them. */
const SLOT = { cpu: 1, rx: 2, mem: 3, disk: 4, tx: 6 } as const;
const DISK_SLOT: Record<Exclude<DiskPartId, 'free'>, number> = {
  workspace: 1,
  caches: 2,
  agents: 4,
  other: 6,
};

export interface IProcRow {
  key: string;
  proc: MachineProc;
  label: string;
  sub: string;
  text: string;
  mem: string;
  tone: '' | 'bad';
  vars: TVars;
}

export interface IMachineModel {
  level: { tone: TTone; text: string };
  kpis: IKpi[];
  load: { legend: { label: string; slot: number }[]; plot: IPlot };
  cores: IHeatCell[];
  net: { legend: { label: string; slot: number }[]; plot: IPlot };
  disk: {
    title: string;
    parts: { id: DiskPartId; vars: TVars; label: string; text: string; free: boolean }[];
    /** Said under the legend while the folders are not counted (yet) or were counted in part. */
    note: string | null;
  } | null;
}

const share = (x: number) => Math.round(x * 100);

const worst = (...levels: TTone[]): TTone =>
  levels.reduce((a, b) => (LEVEL_RANK[b] > LEVEL_RANK[a] ? b : a), TONE.ok);

const levelText = (level: TTone): string => t(`next.machine.level.${level}`);

/** The size of memory in use out of the total, in one unit when both share it: «11,2 из 32 ГБ». */
const memSub = (used: number, total: number): string => {
  const a = bytesParts(used);
  const b = bytesParts(total);
  return a.unit === b.unit
    ? t('next.machine.of', { a: a.value, b: `${b.value} ${b.unit}` })
    : t('next.machine.of', { a: bytes(used), b: bytes(total) });
};

const minutesBack = (to: number, at: number): string => {
  const min = Math.round((to - at) / 60_000);
  return min <= 0 ? t('next.machine.axis.now') : t('next.machine.axis.back', { n: min });
};

const AXIS_LOAD = ['m60', 'm45', 'm30', 'm15', 'now'] as const;
const AXIS_NET = ['m60', 'm30', 'now'] as const;
const axisLabels = (keys: readonly string[]) => keys.map((k) => t(`next.machine.axis.${k}`));

export const procRow = (proc: MachineProc, index: number): IProcRow => {
  const [item] = list(
    [
      {
        key: `${proc.pid}:${proc.startTime}`,
        label: proc.name,
        sub: '',
        value: proc.cpu,
        text: percent(proc.cpu),
        tone: proc.stuck ? 'bad' : '',
      },
    ],
    100,
  );
  const sub = [
    proc.command,
    proc.port === null ? '' : t('next.machine.port', { port: proc.port }),
    proc.stuck ? t('next.machine.stuck') : '',
  ]
    .filter(Boolean)
    .join(' · ');
  return {
    key: item.key,
    proc,
    label: proc.name,
    sub,
    text: item.text,
    mem: bytes(proc.rss),
    tone: item.tone,
    vars: { ...item.vars, i: index },
  };
};

export const buildMachine = (stats: MachineStats): IMachineModel => {
  const history = stats.history;
  const to = history.length ? history[history.length - 1].at : stats.at;
  const from = to - SPAN_MS;
  const times = history.map((x) => x.at);
  const titles = times.map((at) => minutesBack(to, at));
  const cpuH = history.map((x) => share(x.cpu));
  const memH = history.map((x) => share(x.mem));
  const rxH = history.map((x) => x.rx);
  const txH = history.map((x) => x.tx);

  const cpu = share(stats.cpu);
  const mem = stats.mem.total > 0 ? share(stats.mem.used / stats.mem.total) : 0;
  const diskTotal = stats.disk.total;
  const diskUsed = diskTotal > 0 ? share((diskTotal - stats.disk.free) / diskTotal) : 0;
  const level = worst(
    tone(cpu, CPU_LEVELS.warn, CPU_LEVELS.bad),
    tone(mem, MEM_LEVELS.warn, MEM_LEVELS.bad),
    tone(diskUsed, DISK_LEVELS.warn, DISK_LEVELS.bad),
  );

  const netNow = rateParts(stats.net.rx);
  const kpis: IKpi[] = [
    kpi({
      label: t('next.machine.cpu'),
      value: num(cpu),
      unit: '%',
      index: 0,
      delta:
        cpuH.length > DELTA_BACK
          ? delta(cpu, cpuH[cpuH.length - 1 - DELTA_BACK], t('next.machine.points'), 'down')
          : undefined,
      sub: plural(stats.cores.length, 'next.machine.cores'),
      spark: cpuH.slice(-SPARK_POINTS),
      sparkMin: 0,
      sparkMax: 100,
      tone: tone(cpu, CPU_LEVELS.warn, CPU_LEVELS.bad),
    }),
    kpi({
      label: t('next.machine.mem'),
      value: num(mem),
      unit: '%',
      index: 1,
      sub: memSub(stats.mem.used, stats.mem.total),
      spark: memH.slice(-SPARK_POINTS),
      sparkMin: 0,
      sparkMax: 100,
      slot: SLOT.mem,
    }),
    ...(diskTotal > 0
      ? [
          kpi({
            label: t('next.machine.disk'),
            value: num(diskUsed),
            unit: '%',
            index: 2,
            sub: t('next.machine.free', { size: bytes(stats.disk.free) }),
            slot: SLOT.disk,
          }),
        ]
      : []),
    kpi({
      label: t('next.machine.net'),
      value: netNow.value,
      unit: ` ${netNow.unit}`,
      index: 3,
      sub: `↑ ${rate(stats.net.tx)}`,
      spark: rxH.slice(-SPARK_POINTS),
      slot: SLOT.rx,
    }),
  ];

  const loadSeries = [
    { name: t('next.machine.cpu'), values: cpuH, slot: SLOT.cpu, format: percent },
    { name: t('next.machine.mem'), values: memH, slot: SLOT.mem, format: percent },
  ];
  const netSeries = [
    { name: t('next.machine.rx'), values: rxH, slot: SLOT.rx, format: rate },
    { name: t('next.machine.tx'), values: txH, slot: SLOT.tx, format: rate },
  ];

  const parts = stats.disk.parts.filter((x) => x.bytes > 0);
  const disk =
    diskTotal > 0
      ? {
          title: t('next.machine.diskTitle', { total: bytes(diskTotal) }),
          parts: stack(
            parts.map((x) => ({
              id: x.id,
              label: t(`next.machine.part.${x.id}`),
              value: x.bytes,
              ...(x.id === 'free' ? { free: true } : { slot: DISK_SLOT[x.id] }),
            })),
            diskTotal,
            bytes,
          ).parts,
          note:
            stats.disk.measuredAt === null
              ? t('next.machine.diskCounting')
              : stats.disk.partial
                ? t('next.machine.diskPartial')
                : null,
        }
      : null;

  return {
    level: { tone: level, text: levelText(level) },
    kpis,
    load: {
      legend: loadSeries.map((s) => ({ label: s.name, slot: s.slot })),
      plot: plot({
        series: loadSeries,
        times,
        from,
        to,
        titles,
        xs: axisLabels(AXIS_LOAD),
        min: 0,
        max: 100,
        yTicks: 5,
        yFormat: percent,
        refs: [{ value: CPU_LEVELS.bad, label: t('next.machine.limit', { n: CPU_LEVELS.bad }) }],
      }),
    },
    cores: heat(
      stats.cores.map((x) => share(x)),
      100,
      (i) => t('next.machine.core', { n: i + 1 }),
      percent,
    ),
    net: {
      legend: netSeries.map((s) => ({ label: s.name, slot: s.slot })),
      plot: plot({
        series: netSeries,
        times,
        from,
        to,
        titles,
        xs: axisLabels(AXIS_NET),
        min: 0,
        yTicks: 3,
        yFormat: rate,
      }),
    },
    disk,
  };
};
