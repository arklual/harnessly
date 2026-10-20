import type { IEvalAlert, IEvalRuns, IEvalSchedule, TEvalRunState } from '@/types/efficiency';
import type { TTone } from '@/components/agents/efficiency/families-math';
import { t } from '@/lib/i18n';

export const RUN_STATE_LABEL: Record<TEvalRunState, string> = {
  queued: t('efficiency.runsMath.stateQueued'),
  claimed: t('efficiency.runsMath.stateClaimed'),
  preparing: t('efficiency.runsMath.statePreparing'),
  running: t('efficiency.runsMath.stateRunning'),
  grading: t('efficiency.runsMath.stateGrading'),
  done: t('efficiency.runsMath.stateDone'),
  failed: t('efficiency.runsMath.stateFailed'),
  timeout: t('efficiency.runsMath.stateTimeout'),
  stopped: t('efficiency.runsMath.stateStopped'),
  skipped: t('efficiency.runsMath.stateSkipped'),
};

export const runTone = (state: string): TTone => {
  if (state === 'done') return 'ok';
  if (state === 'failed' || state === 'timeout') return 'bad';
  if (state === 'stopped' || state === 'skipped') return 'flat';
  return 'warn';
};

export const ALERT_STATES = [
  { value: 'explained', label: t('efficiency.runsMath.alertExplained') },
  { value: 'fixed', label: t('efficiency.runsMath.alertFixed') },
  { value: 'false_alarm', label: t('efficiency.runsMath.alertFalseAlarm') },
] as const;

export const isOpenAlert = (a: Pick<IEvalAlert, 'state'>): boolean => a.state === 'open';

/** Alerts addressed to the viewer that nobody has closed yet: they light the sidebar dot, the tab and the banner. */
export const mineOpenAlerts = (runs: Pick<IEvalRuns, 'alerts'> | null): IEvalAlert[] =>
  runs ? runs.alerts.filter((a) => a.mine && isOpenAlert(a)) : [];

export interface ISmokePoint {
  index: number;
  at: string;
  passed: number;
  total: number;
  usdPerTask: number | null;
  fingerprint: string;
  /** Outside the A/A band: worth a look. */
  outside: boolean;
  /** The harness fingerprint differs from the previous night: something changed in between. */
  changed: boolean;
}

export const smokePoints = (runs: Pick<IEvalRuns, 'smoke' | 'band'>): ISmokePoint[] =>
  [...runs.smoke]
    .sort((a, b) => Date.parse(a.at) - Date.parse(b.at))
    .map((p, index, all) => ({
      ...p,
      index,
      outside: runs.band !== null && (p.passed < runs.band[0] || p.passed > runs.band[1]),
      changed: index > 0 && all[index - 1].fingerprint !== p.fingerprint,
    }));

export const smokeMedian = (points: ISmokePoint[], last = 7): number | null => {
  const values = points
    .slice(-last)
    .map((p) => p.passed)
    .sort((a, b) => a - b);
  if (values.length === 0) return null;
  const mid = values.length >> 1;
  return values.length % 2 ? values[mid] : (values[mid - 1] + values[mid]) / 2;
};

export const scheduleTotals = (schedules: IEvalSchedule[]): { runsPerWeek: number; weekPct: number } => ({
  runsPerWeek: schedules.filter((s) => s.enabled).reduce((a, s) => a + s.runsPerWeek, 0),
  weekPct: schedules.filter((s) => s.enabled).reduce((a, s) => a + s.weekPct, 0),
});

export const batchStateLabel = (state: string): string =>
  state in RUN_STATE_LABEL ? RUN_STATE_LABEL[state as TEvalRunState] : state;
