import { locale, t } from '@/lib/i18n';
import { scopedAppPath } from '@/lib/app-path';
import { THRESHOLDS, type TLoadLevel, type TMetric } from '@/lib/hl-machine-load';
import type { IMachineLoad, IMachineProcess } from '@/types/server/hl-machine-load-scan';

/** What the browser shows of the machine load: wording, tones, the overload gate and stop outcomes. Pure. */

export type TLoadTone = 'success' | 'warning' | 'danger';

export const LEVEL_TONE: Record<TLoadLevel, TLoadTone> = { ok: 'success', warn: 'warning', over: 'danger' };

export const LEVEL_LABEL: Record<TLoadLevel, string> = {
  ok: t('lib.hlMachineLoadView.levelOk'),
  warn: t('lib.hlMachineLoadView.levelWarn'),
  over: t('lib.hlMachineLoadView.levelOver'),
};

/** One decimal with the separator of the page's language. */
const decimal = (value: number): string =>
  value.toFixed(1).replace('.', (1.5).toLocaleString(locale()).charAt(1));

export const formatPct = (value: number): string =>
  t('lib.hlMachineLoadView.percent', { value: Math.round(value) });

const GB = 1024 ** 3;
const MB = 1024 ** 2;

export const formatBytes = (bytes: number): string => {
  if (bytes >= GB) return t('lib.hlMachineLoadView.gigabytes', { value: decimal(bytes / GB) });
  if (bytes >= MB) return t('lib.hlMachineLoadView.megabytes', { value: Math.round(bytes / MB) });
  return t('lib.hlMachineLoadView.kilobytes', { value: Math.round(bytes / 1024) });
};

/** load1 per core, the figure the thresholds compare. */
export const loadPerCore = (load: Pick<IMachineLoad, 'cpu'>): number =>
  load.cpu.cores > 0 ? load.cpu.load1 / load.cpu.cores : 0;

export const loadRatioText = (load: Pick<IMachineLoad, 'cpu'>): string => `${decimal(loadPerCore(load))}×`;

export const metricText = (metric: TMetric, load: IMachineLoad): string => {
  if (metric === 'ram') return `RAM ${formatPct(load.mem.usedPct)}`;
  if (metric === 'load') return t('lib.hlMachineLoadView.metricLoad', { ratio: loadRatioText(load) });
  if (metric === 'disk')
    return t('lib.hlMachineLoadView.metricDisk', { percent: formatPct(load.disk.usedPct) });
  return t('lib.hlMachineLoadView.metricSwap');
};

/** The short figure next to the indicator: the metric behind the level, RAM when all is well. */
export const indicatorText = (load: IMachineLoad): string => metricText(load.reason ?? 'ram', load);

const metricValue = (metric: TMetric, load: IMachineLoad): number | null => {
  if (metric === 'ram') return load.mem.usedPct;
  if (metric === 'load') return loadPerCore(load);
  if (metric === 'disk') return load.disk.usedPct;
  return load.mem.swapInPerSec;
};

/** Tone of one metric by the entry thresholds, without the server's hysteresis: a bar colours what it shows. */
export const metricTone = (metric: TMetric, load: IMachineLoad): TLoadTone => {
  const value = metricValue(metric, load);
  if (value === null) return LEVEL_TONE.ok;
  if (value >= THRESHOLDS[metric].over) return LEVEL_TONE.over;
  if (value >= THRESHOLDS[metric].warn) return LEVEL_TONE.warn;
  return LEVEL_TONE.ok;
};

export const swapInText = (perSec: number | null): string =>
  perSec === null
    ? t('lib.hlMachineLoadView.swapInCounting')
    : t('lib.hlMachineLoadView.swapInRate', { rate: Math.round(perSec) });

export const overloadQuestion = (load: IMachineLoad): string =>
  t('lib.hlMachineLoadView.overloadQuestion', { metric: metricText(load.reason ?? 'ram', load) });

export type TGateDecision = 'open' | 'confirm';

/** A new port or address on an overloaded machine waits for a «yes»; reopening what is already shown never does. */
export const gateDecision = (
  level: TLoadLevel | null,
  targetKey: string,
  shownKey: string | null,
): TGateDecision => (level === 'over' && targetKey !== shownKey ? 'confirm' : 'open');

/** Auto-open of a freshly started dev server stays off while the machine is overloaded. */
export const autoOpenAllowed = (level: TLoadLevel | null): boolean => level !== 'over';

/** Toast once per entry into overload; the first sample counts as an entry. */
export const enteredOverload = (previous: TLoadLevel | null, next: TLoadLevel): boolean =>
  next === 'over' && previous !== 'over';

export const LIST_CHANGED = t('lib.hlMachineLoadView.listChanged');

export type TStopOutcome =
  { kind: 'stopped' } | { kind: 'refresh'; message: string } | { kind: 'error'; message: string };

const STOP_ERRORS: Record<string, string> = {
  protected: t('lib.hlMachineLoadView.stopProtected'),
  foreign: t('lib.hlMachineLoadView.stopForeign'),
  'not-stoppable': t('lib.hlMachineLoadView.stopNotStoppable'),
  unauthorized: t('lib.hlMachineLoadView.stopUnauthorized'),
  'cross-site': t('lib.hlMachineLoadView.stopCrossSite'),
};

const REFRESH_ERRORS = new Set(['not-listed', 'gone', 'changed']);

export const stopOutcome = (status: number, body: unknown): TStopOutcome => {
  const error =
    typeof body === 'object' && body !== null && 'error' in body && typeof body.error === 'string'
      ? body.error
      : null;
  if (status === 200 && error === null) return { kind: 'stopped' };
  if (error !== null && REFRESH_ERRORS.has(error)) return { kind: 'refresh', message: LIST_CHANGED };
  return {
    kind: 'error',
    message: (error !== null && STOP_ERRORS[error]) || t('lib.hlMachineLoadView.stopFailed', { status }),
  };
};

export const processKey = (proc: Pick<IMachineProcess, 'pid' | 'starttime'>): string =>
  `${proc.pid}:${proc.starttime}`;

export const processForPort = (
  top: readonly IMachineProcess[] | undefined,
  port: number,
): IMachineProcess | null => top?.find((proc) => proc.port === port) ?? null;

export const processUsage = (proc: Pick<IMachineProcess, 'cpuPct' | 'rssBytes'>): string =>
  `CPU ${formatPct(proc.cpuPct)} · ${formatBytes(proc.rssBytes)}`;

/** One key per machine on a page: `''` (the page's own runtime) and the runtime's `?machine=` id are the same machine. */
export const machineKey = (machine: string, pageMachine: string | undefined): string =>
  machine || pageMachine || '';

/**
 * Every known machine goes by `?machine=`, the page's own too: without it the gateway may answer from whichever fleet
 * machine is live and show another machine's load. Only `''` (no machine known at all) goes unscoped.
 */
export const machineLoadPath = (path: string, key: string): string => scopedAppPath(path, key || undefined);

/** A load older than this, or one whose last poll failed, says nothing about the present. */
export const LOAD_STALE_MS = 15_000;

export const isLoadStale = (receivedAt: number | null, failed: boolean, now: number): boolean =>
  receivedAt === null || failed || now - receivedAt > LOAD_STALE_MS;

/** The level the gate and auto-open act on: a stale load counts as unknown. */
export const effectiveLevel = (snapshot: {
  load: Pick<IMachineLoad, 'level'> | null;
  stale: boolean;
}): TLoadLevel | null => (snapshot.load && !snapshot.stale ? snapshot.load.level : null);

/** A stopped process stays marked until it leaves the list or its mark expires. */
export const pruneStopped = (
  stopped: ReadonlyMap<string, number>,
  top: readonly IMachineProcess[],
  now: number,
): Map<string, number> => {
  const listed = new Set(top.map(processKey));
  return new Map([...stopped].filter(([key, until]) => listed.has(key) && until > now));
};
