import { GITHUB_KEY_RE } from '@/lib/site';
import { useCallback, useEffect, useState } from 'react';
import { appFetch } from '@/lib/app-path';
import type { ITask, TTaskError } from '@/lib/hl-task';

export type TTaskLoad =
  { status: 'loading' } | { status: 'ready'; task: ITask } | { status: 'error'; error: TTaskError };

const ERRORS: readonly TTaskError[] = ['not-found', 'no-token', 'tracker'];

const request = async (key: string, signal: AbortSignal): Promise<TTaskLoad> => {
  try {
    const route = GITHUB_KEY_RE.test(key) ? '/api/platform/github/task' : '/api/platform/task';
    const response = await appFetch(`${route}?${new URLSearchParams({ key })}`, { signal });
    if (response.ok) return { status: 'ready', task: (await response.json()) as ITask };
    const said = await response
      .json()
      .then((body: { error?: unknown }) => body?.error)
      .catch(() => null);
    return { status: 'error', error: ERRORS.find((known) => known === said) ?? 'tracker' };
  } catch {
    return { status: 'error', error: 'tracker' };
  }
};

/** A task of the tracker for the tab: loaded when the key changes, again on `reload`. */
const useTask = (key: string | null): [TTaskLoad, () => void] => {
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<{ key: string; attempt: number; state: TTaskLoad } | null>(null);
  useEffect(() => {
    if (!key) return;
    const controller = new AbortController();
    void request(key, controller.signal).then((state) => {
      if (!controller.signal.aborted) setLoad({ key, attempt, state });
    });
    return () => controller.abort();
  }, [key, attempt]);
  const reload = useCallback(() => setAttempt((value) => value + 1), []);
  return [load && load.key === key && load.attempt === attempt ? load.state : { status: 'loading' }, reload];
};

export default useTask;
