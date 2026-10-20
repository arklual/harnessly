import { useEffect, useState } from 'react';

/**
 * A machine missing from one fleet answer is usually late, not down: the gateway's first list on a cold start
 * leaves out whoever answers after 2.5 s, and the follow-up 4 s later has it. Only a machine still missing after
 * `afterMs` is worth a banner. Errors are `"<machine>: <reason>"`; the reason may change between answers.
 */
const machineOf = (error: string): string => error.split(':', 1)[0];

/** Since when each machine of `errors` has been missing, keeping the start of an outage that goes on. */
export const missingSince = (
  prev: Record<string, number>,
  errors: readonly string[],
  now: number,
): Record<string, number> => {
  const next: Record<string, number> = {};
  for (const error of errors) next[machineOf(error)] = prev[machineOf(error)] ?? now;
  return next;
};

export const lastingOf = (
  errors: readonly string[],
  since: Record<string, number>,
  now: number,
  afterMs: number,
): string[] =>
  errors.filter((error) => {
    const at = since[machineOf(error)];
    return at !== undefined && now - at >= afterMs;
  });

const useLastingErrors = (
  errors: readonly string[],
  afterMs: number,
  now: () => number = Date.now,
): string[] => {
  // The outage starts are derived from the previous answer during render, the pattern React gives for state that
  // follows a prop (`errors` keeps its identity while the text stays the same).
  const [seen, setSeen] = useState(errors);
  const [since, setSince] = useState(() => missingSince({}, errors, now()));
  if (errors !== seen) {
    setSeen(errors);
    setSince(missingSince(since, errors, now()));
  }
  const [, setTick] = useState(0);

  const pending = Object.values(since).filter((at) => now() - at < afterMs);
  const nextDue = pending.length ? Math.min(...pending) + afterMs - now() : null;
  useEffect(() => {
    if (nextDue === null) return;
    const timer = setTimeout(() => setTick((tick) => tick + 1), Math.max(0, nextDue) + 20);
    return () => clearTimeout(timer);
  }, [nextDue]);

  return lastingOf(errors, since, now(), afterMs);
};

export default useLastingErrors;
