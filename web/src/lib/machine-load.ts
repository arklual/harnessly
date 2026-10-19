import type { MachineLoad } from '../api/types';

type Metric = 'ram' | 'swap' | 'load' | 'disk';
export type LoadTone = 'success' | 'warning' | 'danger';

// Where the runtime's machine load scan enters the warning and overload levels.
const THRESHOLDS: Record<Metric, { warn: number; over: number }> = {
  ram: { warn: 85, over: 93 },
  swap: { warn: 50, over: 500 },
  load: { warn: 1.5, over: 3 },
  disk: { warn: 90, over: 97 },
};

export const LEVEL_TONE: Record<MachineLoad['level'], LoadTone> = {
  ok: 'success',
  warn: 'warning',
  over: 'danger',
};
export const LEVEL_LABEL: Record<MachineLoad['level'], string> = {
  ok: 'Load is normal',
  warn: 'Machine under load',
  over: 'Machine overloaded',
};

/** load1 per core, the figure the thresholds compare. */
export const loadPerCore = (load: Pick<MachineLoad, 'cpu'>): number =>
  load.cpu.cores > 0 ? load.cpu.load1 / load.cpu.cores : 0;

const metricValue = (metric: Metric, load: MachineLoad): number | null => {
  if (metric === 'ram') return load.mem.usedPct;
  if (metric === 'load') return loadPerCore(load);
  if (metric === 'disk') return load.disk.usedPct;
  return load.mem.swapInPerSec;
};

/** A bar's tone by the entry thresholds alone: it colours what it shows. */
export const metricTone = (metric: Metric, load: MachineLoad): LoadTone => {
  const value = metricValue(metric, load);
  if (value === null) return 'success';
  if (value >= THRESHOLDS[metric].over) return 'danger';
  if (value >= THRESHOLDS[metric].warn) return 'warning';
  return 'success';
};

/** The fractions (0..1) at which a percentage bar of this metric turns. */
export const meterSteps = (metric: 'ram' | 'disk') => ({
  warnAt: THRESHOLDS[metric].warn / 100,
  dangerAt: THRESHOLDS[metric].over / 100,
});

const STOP_ERRORS: Record<string, string> = {
  protected: 'This process is protected and cannot be stopped',
  foreign: "Another user's process cannot be stopped",
  'not-stoppable': 'This process cannot be stopped from here',
  unauthorized: 'The session has expired; sign in again',
  'cross-site': 'Refused: a process can only be stopped from this page',
};
const REFRESH_ERRORS = new Set(['not-listed', 'gone', 'changed']);

export type StopOutcome =
  { kind: 'stopped' } | { kind: 'refresh'; message: string } | { kind: 'error'; message: string };

export const stopOutcome = (status: number, body: unknown): StopOutcome => {
  const error =
    typeof body === 'object' && body !== null && 'error' in body && typeof body.error === 'string'
      ? body.error
      : null;
  if (status === 200 && error === null) return { kind: 'stopped' };
  if (error !== null && REFRESH_ERRORS.has(error))
    return { kind: 'refresh', message: 'The process list changed; refreshed' };
  return {
    kind: 'error',
    message: (error !== null && STOP_ERRORS[error]) || `The process could not be stopped (${status})`,
  };
};
