import { plural, t } from '@/lib/i18n';

const pad2 = (n: number) => String(n).padStart(2, '0');

export const formatClock = (ms: number): string => {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return h > 0 ? `${h}:${pad2(m)}:${pad2(s)}` : `${m}:${pad2(s)}`;
};

export const formatSpan = (ms: number): string => {
  const total = Math.max(1, Math.round(ms / 1000));
  if (total < 60) return t('lib.turnFormat.seconds', { n: total });
  const minutes = Math.floor(total / 60);
  if (minutes < 60) return t('lib.turnFormat.minutes', { n: minutes });
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m > 0 ? t('lib.turnFormat.hoursMinutes', { h, m }) : t('lib.turnFormat.hours', { h });
};

/** The folded turn's one line: "Worked 2m · 14 actions", or "Stopped after …" for a stopped one. */
export const workedLabel = (ms: number, actions: number, interrupted: boolean): string => {
  const head = t(interrupted ? 'lib.turnFormat.stoppedAfter' : 'lib.turnFormat.worked', {
    span: formatSpan(ms),
  });
  return actions > 0 ? `${head} · ${plural(actions, 'lib.turnFormat.actions')}` : head;
};
