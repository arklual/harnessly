// A space's live data: the machines (fetched, then kept by the SSE stream) and the progress of installs and logins
// (which exist only as events). The stream has no replay, so the UI refetches on every (re)connect; this file is
// the pure part, the reducer.
import type { Job, Login, Machine, SpaceEvent } from './types';

export interface LiveState {
  /** null until the first answer: a space with no machines is [] (loading is not empty). */
  machines: Machine[] | null;
  jobs: Record<string, { machineId: string; job: Job }>;
  logins: Record<string, { machineId: string; login: Login }>;
  /** Bumped when the members or invites changed, so the settings page refetches them. */
  membersTick: number;
}

export const initialLive: LiveState = { machines: null, jobs: {}, logins: {}, membersTick: 0 };

export type LiveAction =
  | { type: 'loaded'; machines: Machine[] }
  | { type: 'event'; event: SpaceEvent }
  | { type: 'job'; machineId: string; job: Job }
  | { type: 'login'; machineId: string; login: Login };

const upsert = (list: Machine[] | null, machine: Machine): Machine[] => {
  const rows = list ?? [];
  const at = rows.findIndex((m) => m.id === machine.id);
  if (at < 0) return [...rows, machine];
  return rows.map((m, i) => (i === at ? machine : m));
};

/** A job or login that arrives older than the one held (a replayed or reordered event) does not step it back. */
const JOB_RANK = { running: 0, done: 1, failed: 1 } as const;
const LOGIN_FINAL: ReadonlySet<Login['state']> = new Set(['done', 'failed', 'expired', 'cancelled']);

export const reduceLive = (state: LiveState, action: LiveAction): LiveState => {
  switch (action.type) {
    case 'loaded':
      return { ...state, machines: action.machines };
    case 'job': {
      const held = state.jobs[action.job.id];
      if (held && JOB_RANK[held.job.state] > JOB_RANK[action.job.state]) return state;
      return {
        ...state,
        jobs: { ...state.jobs, [action.job.id]: { machineId: action.machineId, job: action.job } },
      };
    }
    case 'login': {
      const held = state.logins[action.login.id];
      if (held && LOGIN_FINAL.has(held.login.state) && !LOGIN_FINAL.has(action.login.state)) return state;
      return {
        ...state,
        logins: { ...state.logins, [action.login.id]: { machineId: action.machineId, login: action.login } },
      };
    }
    case 'event': {
      const ev = action.event;
      switch (ev.type) {
        case 'machine':
          return { ...state, machines: upsert(state.machines, ev.data.machine) };
        case 'machineRemoved':
          return { ...state, machines: (state.machines ?? []).filter((m) => m.id !== ev.data.machineId) };
        case 'job':
          return reduceLive(state, { type: 'job', machineId: ev.data.machineId, job: ev.data.job });
        case 'login':
          return reduceLive(state, { type: 'login', machineId: ev.data.machineId, login: ev.data.login });
        case 'members':
          return { ...state, membersTick: state.membersTick + 1 };
      }
    }
  }
};

export const EVENT_TYPES = ['machine', 'machineRemoved', 'job', 'login', 'members'] as const;

/** Reads the `data:` line of one SSE message; null when it is not the JSON of the contract. */
export const parseSpaceEvent = (type: string, data: string): SpaceEvent | null => {
  if (!(EVENT_TYPES as readonly string[]).includes(type)) return null;
  try {
    const parsed: unknown = JSON.parse(data);
    if (!parsed || typeof parsed !== 'object') return null;
    return { type, data: parsed } as SpaceEvent;
  } catch {
    return null;
  }
};

export const jobsOf = (state: LiveState, machineId: string): Job[] =>
  Object.values(state.jobs)
    .filter((j) => j.machineId === machineId)
    .map((j) => j.job);

/** The install job of an agent on a machine that arrived last. */
export const installJobOf = (state: LiveState, machineId: string, provider: string): Job | null => {
  const rows = jobsOf(state, machineId).filter((j) => j.kind === 'agent-install' && j.provider === provider);
  return rows.length ? (rows[rows.length - 1] ?? null) : null;
};

export const loginOf = (state: LiveState, machineId: string, provider: string): Login | null => {
  const rows = Object.values(state.logins)
    .filter((l) => l.machineId === machineId && l.login.provider === provider)
    .map((l) => l.login);
  return rows.length ? (rows[rows.length - 1] ?? null) : null;
};
