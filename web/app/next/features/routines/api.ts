// The person's routines (docs/hub/routines.md): recurring agent tasks the hub keeps and runs. The page asks
// its runtime (`<platform>/routines`), the runtime's agent signs the call and the hub answers for the machine's owner.
import { useCallback, useEffect, useState } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

export type TOutcome = 'ok' | 'quiet' | 'failed';
export type TWhen = 'changed' | 'always' | 'failure' | 'never';
export type TAutonomy = 'read' | 'draft' | 'act';
export type TChannel = 'notice' | 'telegram' | 'messenger' | 'mail' | 'file' | 'doc';

export const CHANNELS: readonly TChannel[] = ['notice', 'telegram', 'messenger', 'mail', 'file', 'doc'];
export const WHENS: readonly TWhen[] = ['changed', 'always', 'failure', 'never'];
export const AUTONOMIES: readonly TAutonomy[] = ['read', 'draft', 'act'];
export const MODELS = ['', 'sonnet', 'haiku', 'opus'] as const;

export interface ICheck {
  kind: 'contains' | 'regex' | 'artifact' | 'file' | 'fresh';
  value?: string;
  label?: string;
}

export interface IRoutineSpec {
  words?: string;
  autonomy?: TAutonomy;
  once?: boolean;
  until?: string;
  budgetUsd?: number;
  chatId?: string;
  delivery: { when?: TWhen; channels?: TChannel[]; file?: string };
  checks?: ICheck[];
}

export interface IRunDot {
  id: string;
  at: string;
  status: string;
  outcome?: TOutcome;
}

export interface IRoutine {
  id: string;
  name: string;
  promptTemplate: string;
  model: string;
  enabled: boolean;
  pausedReason?: string;
  state?: string;
  failStreak?: number;
  machineId?: string;
  createdAt: string;
  updatedAt: string;
  trigger: { schedule?: { cron: string; tz?: string } };
  routine: IRoutineSpec;
  next: string[];
  last: IRunDot[];
  stats: { runs: number; ok: number; quiet: number; failed: number };
}

export interface IArtifact {
  kind: string;
  title: string;
  url?: string;
  meta?: string;
}

export interface ICheckResult {
  label: string;
  ok: boolean;
  out?: string;
}

export interface IRunResult {
  summary?: string;
  body?: string;
  artifacts?: IArtifact[];
  checks?: ICheckResult[];
  delivered?: string[];
  error?: string;
  model?: string;
  manual?: boolean;
  said?: TOutcome;
}

export interface IRun {
  id: string;
  ruleId: string;
  fingerprint: string;
  status: string;
  outcome?: TOutcome;
  eventPayload?: unknown;
  conversationId?: string;
  machineId?: string;
  summary?: string;
  error?: string;
  createdAt: string;
  dispatchedAt?: string;
  finishedAt?: string;
  result?: IRunResult;
}

export interface IRoutinesList {
  routines: IRoutine[];
  defaultModel: string;
}

const base = () => platformPath('/routines');
const json = (body: unknown): RequestInit => ({
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify(body),
});

export const listRoutines = (m: MachineId) => runtimeJson<IRoutinesList>(m, base());

export const routineRuns = (m: MachineId, id: string) =>
  runtimeJson<{ runs: IRun[] }>(m, `${base()}/${encodeURIComponent(id)}/runs?limit=60` as `/${string}`);

export interface IRoutinePatch {
  enabled?: boolean;
  model?: string;
  state?: string;
  routine?: IRoutineSpec;
}

export const patchRoutine = (m: MachineId, id: string, patch: IRoutinePatch) =>
  runtimeJson<{ routine: IRoutine }>(m, `${base()}/${encodeURIComponent(id)}` as `/${string}`, {
    method: 'PATCH',
    ...json(patch),
  });

export const runRoutine = (m: MachineId, id: string) =>
  runtimeJson<{ queued: boolean; runId?: string }>(
    m,
    `${base()}/${encodeURIComponent(id)}/run` as `/${string}`,
    {
      method: 'POST',
    },
  );

export const deleteRoutine = (m: MachineId, id: string) =>
  runtimeJson<{ deleted: boolean }>(m, `${base()}/${encodeURIComponent(id)}` as `/${string}`, {
    method: 'DELETE',
  });

type TLoad<T> = { k: 'loading' } | { k: 'ready'; v: T } | { k: 'failed'; error: string };

/** The routines, read again every 30 s while the page is open and at once after a change (`reload`). */
export const useRoutines = (m: MachineId) => {
  const [state, setState] = useState<TLoad<IRoutinesList>>({ k: 'loading' });
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  useEffect(() => {
    let live = true;
    const read = () =>
      listRoutines(m).then(
        (v) => live && setState({ k: 'ready', v }),
        (e: unknown) =>
          live &&
          setState((s) => (s.k === 'ready' ? s : { k: 'failed', error: String((e as Error)?.message ?? e) })),
      );
    void read();
    const t = window.setInterval(read, 30_000);
    return () => {
      live = false;
      window.clearInterval(t);
    };
  }, [m, tick]);
  return { state, reload };
};

/** One routine's runs; read again with the list. Runs of another routine read before are not shown for this one. */
export const useRuns = (m: MachineId, id: string | null, tick: unknown) => {
  const [got, setGot] = useState<{ id: string; runs: IRun[] } | null>(null);
  useEffect(() => {
    if (!id) return;
    let live = true;
    routineRuns(m, id).then(
      (v) => live && setGot({ id, runs: v.runs }),
      () => live && setGot((g) => (g && g.id === id ? g : { id, runs: [] })),
    );
    return () => {
      live = false;
    };
  }, [m, id, tick]);
  return got && got.id === id ? got.runs : null;
};
