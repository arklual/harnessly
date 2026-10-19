/** The tone of a share of a limit: ok, warn from warnAt, danger from dangerAt. */
export function meterTone(value: number, warnAt = 0.7, dangerAt = 0.9): 'ok' | 'warn' | 'danger' {
  if (value >= dangerAt) return 'danger';
  if (value >= warnAt) return 'warn';
  return 'ok';
}
