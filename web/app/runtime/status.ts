import type { StatusEntry } from './types';

export interface StatusState {
  tabs: Record<string, StatusEntry>;
  /** The raw rate limits file the runtime last sent, when it knows one. */
  rateLimits: unknown;
}

export const emptyStatus: StatusState = { tabs: {}, rateLimits: null };

type Msg = { type?: unknown; [k: string]: unknown };

/** The status socket's messages folded into the tabs' states. Unknown messages leave the state as it is. */
export function reduceStatus(state: StatusState, msg: Msg): StatusState {
  switch (msg.type) {
    case 'status:sync': {
      const tabs = msg.tabs && typeof msg.tabs === 'object' ? (msg.tabs as Record<string, StatusEntry>) : {};
      return { ...state, tabs: { ...tabs } };
    }
    case 'status:update': {
      const { type: _t, tabId, ...entry } = msg;
      if (typeof tabId !== 'string') return state;
      const next = { ...state.tabs };
      // A removed tab comes as an entry with no state, workspace or name.
      if (entry.cliState === null && entry.workspaceId === '' && entry.tabName === '') delete next[tabId];
      else next[tabId] = entry as unknown as StatusEntry;
      return { ...state, tabs: next };
    }
    case 'status:hook-event': {
      const tabId = msg.tabId;
      const event = msg.event as StatusEntry['lastEvent'];
      if (typeof tabId !== 'string' || !event) return state;
      const cur = state.tabs[tabId];
      if (!cur) return state;
      return {
        ...state,
        tabs: { ...state.tabs, [tabId]: { ...cur, lastEvent: event, eventSeq: event.seq } },
      };
    }
    case 'rate-limits:update':
      return { ...state, rateLimits: msg.data ?? null };
    default:
      return state;
  }
}

export type AgentActivity = 'starting' | 'working' | 'waiting' | 'done' | 'idle' | 'stopped' | 'offline';

/** What a person should read from a tab's state: one word and its tone. */
export function activityOf(entry: StatusEntry | undefined): AgentActivity {
  if (!entry || entry.cliState === null) return 'offline';
  if (entry.launchingSince) return 'starting';
  switch (entry.cliState) {
    case 'busy':
      return 'working';
    case 'needs-input':
      return 'waiting';
    case 'ready-for-review':
      return 'done';
    case 'cancelled':
      return 'stopped';
    case 'inactive':
      return 'starting';
    default:
      return 'idle';
  }
}

export const activityLabel: Record<AgentActivity, string> = {
  starting: 'Starting',
  working: 'Working',
  waiting: 'Needs input',
  done: 'Done',
  idle: 'Idle',
  stopped: 'Stopped',
  offline: 'Not running',
};

export const activityTone = {
  starting: 'info',
  working: 'accent',
  waiting: 'warning',
  done: 'success',
  idle: 'neutral',
  stopped: 'neutral',
  offline: 'neutral',
} as const satisfies Record<AgentActivity, 'info' | 'accent' | 'warning' | 'success' | 'neutral'>;
