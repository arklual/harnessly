// An agent conversation live from the runtime's timeline socket (/api/timeline): the parsed transcript, older pages
// on demand, the checklist folded from task updates, and the session's meta and stats.
import { useCallback, useEffect, useMemo, useReducer, useRef } from 'react';
import { ReconnectingSocket } from '@/runtime/socket';
import { socketUrl } from '@/runtime/base';
import { appFetch } from '@/lib/app-path';
import type { ITaskItem, ITimelineEntry } from '@/types/timeline';
import { t } from '@/lib/i18n';

export type TTimelineSocketStatus = 'connecting' | 'connected' | 'disconnected';

interface IState {
  entries: ITimelineEntry[];
  sessionId: string | null;
  jsonlPath: string | null;
  hasMore: boolean;
  startByteOffset: number;
  initMeta: Record<string, unknown> | null;
  sessionStats: Record<string, unknown> | null;
  liveText: string;
  isLoading: boolean;
  error: string | null;
  wsStatus: TTimelineSocketStatus;
  starting: boolean;
  /** Bringing a stopped session back: asked, refused (why) or idle. */
  resume: { state: 'idle' | 'starting' | 'error'; message: string | null };
}

const initial: IState = {
  entries: [],
  sessionId: null,
  jsonlPath: null,
  hasMore: false,
  startByteOffset: 0,
  initMeta: null,
  sessionStats: null,
  liveText: '',
  isLoading: true,
  error: null,
  wsStatus: 'connecting',
  starting: false,
  resume: { state: 'idle', message: null },
};

type TAction =
  | { kind: 'socket'; status: TTimelineSocketStatus }
  | { kind: 'msg'; msg: Record<string, unknown> }
  | { kind: 'older'; entries: ITimelineEntry[]; startByteOffset: number; hasMore: boolean; replace?: boolean }
  | { kind: 'reset' };

const merge = (known: ITimelineEntry[], more: ITimelineEntry[]) => {
  const ids = new Set(known.map((e) => e.id));
  const fresh = more.filter((e) => !ids.has(e.id));
  return fresh.length ? [...known, ...fresh] : known;
};

const reducer = (state: IState, a: TAction): IState => {
  switch (a.kind) {
    case 'reset':
      return initial;
    case 'socket':
      return { ...state, wsStatus: a.status };
    case 'older':
      return {
        ...state,
        entries: a.replace
          ? a.entries
          : [...a.entries.filter((e) => !state.entries.some((k) => k.id === e.id)), ...state.entries],
        startByteOffset: a.startByteOffset,
        hasMore: a.hasMore,
      };
    case 'msg': {
      const m = a.msg;
      switch (m.type) {
        case 'timeline:init':
          return {
            ...state,
            entries: (m.entries as ITimelineEntry[]) ?? [],
            sessionId: typeof m.sessionId === 'string' && m.sessionId ? m.sessionId : null,
            jsonlPath: typeof m.jsonlPath === 'string' ? m.jsonlPath : null,
            hasMore: m.hasMore === true,
            startByteOffset: typeof m.startByteOffset === 'number' ? m.startByteOffset : 0,
            initMeta: (m.meta as Record<string, unknown>) ?? null,
            sessionStats: (m.sessionStats as Record<string, unknown>) ?? state.sessionStats,
            starting: m.isClaudeStarting === true,
            isLoading: false,
            error: null,
            liveText: '',
          };
        case 'timeline:append':
          return {
            ...state,
            entries: merge(state.entries, (m.entries as ITimelineEntry[]) ?? []),
            liveText: '',
          };
        case 'timeline:live-text':
          return { ...state, liveText: typeof m.text === 'string' ? m.text : '' };
        case 'timeline:stats-update':
          return {
            ...state,
            sessionStats: (m.sessionStats as Record<string, unknown>) ?? state.sessionStats,
          };
        case 'timeline:session-changed':
          return {
            ...state,
            sessionId: typeof m.newSessionId === 'string' && m.newSessionId ? m.newSessionId : null,
            liveText: '',
          };
        case 'timeline:resume-started':
          return { ...state, resume: { state: 'starting', message: null } };
        case 'timeline:resume-error':
        case 'timeline:resume-blocked':
          return {
            ...state,
            resume: {
              state: 'error',
              message: typeof m.message === 'string' ? m.message : t('hooks.useTimeline.resumeFailed'),
            },
          };
        case 'timeline:error':
          return {
            ...state,
            isLoading: false,
            error: typeof m.message === 'string' ? m.message : t('hooks.useTimeline.readFailed'),
          };
        default:
          return state;
      }
    }
  }
};

/** The checklist as the agent's task updates left it. */
export const foldTasks = (entries: readonly ITimelineEntry[]): ITaskItem[] => {
  const tasks: ITaskItem[] = [];
  for (const e of entries) {
    if (e.type !== 'task-progress') continue;
    if (e.action === 'create')
      tasks.push({
        taskId: e.taskId || String(tasks.length + 1),
        subject: e.subject ?? '',
        description: e.description,
        status: e.status,
      });
    else {
      const task = tasks.find((t) => t.taskId === e.taskId);
      if (task) task.status = e.status;
    }
  }
  return tasks;
};

interface IUseTimelineOptions {
  sessionName: string;
  agentSessionId?: string | null;
  panelType: 'claude-code' | 'codex-cli';
  machineId?: string | null;
  enabled?: boolean;
}

export default function useTimeline({
  sessionName,
  agentSessionId,
  panelType,
  enabled = true,
}: IUseTimelineOptions) {
  const [state, dispatch] = useReducer(reducer, initial);
  const sessionRef = useRef(agentSessionId);
  useEffect(() => {
    sessionRef.current = agentSessionId;
  }, [agentSessionId]);
  const [epoch, bump] = useReducer((n: number) => n + 1, 0);
  const socketRef = useRef<ReconnectingSocket | null>(null);

  useEffect(() => {
    if (!enabled) return;
    dispatch({ kind: 'reset' });
    const sock = new ReconnectingSocket({
      url: () => {
        const sid = sessionRef.current;
        const params = new URLSearchParams({ session: sessionName, panelType });
        if (sid) params.set('agentSessionId', sid);
        return socketUrl(`/api/timeline?${params}`);
      },
      onOpen: () => dispatch({ kind: 'socket', status: 'connected' }),
      onState: (s) =>
        dispatch({
          kind: 'socket',
          status: s === 'open' ? 'connected' : s === 'connecting' ? 'connecting' : 'disconnected',
        }),
      onMessage: (data) => {
        if (typeof data !== 'string') return;
        try {
          dispatch({ kind: 'msg', msg: JSON.parse(data) as Record<string, unknown> });
        } catch {
          // not a message of this socket
        }
      },
      shouldRetry: (ev) => ev.code !== 1008,
      minDelay: 1000,
    });
    socketRef.current = sock;
    const beat = window.setInterval(() => sock.send('{"type":"ping"}'), 25_000);
    return () => {
      window.clearInterval(beat);
      sock.close();
      if (socketRef.current === sock) socketRef.current = null;
    };
  }, [sessionName, panelType, enabled, epoch]);

  const loadMore = useCallback(async () => {
    if (!state.jsonlPath || !state.hasMore) return;
    const params = new URLSearchParams({
      jsonlPath: state.jsonlPath,
      beforeByte: String(state.startByteOffset),
      limit: '128',
    });
    const res = await appFetch(`/api/timeline/entries?${params}`);
    if (!res.ok) return;
    const page = (await res.json()) as {
      entries: ITimelineEntry[];
      startByteOffset: number;
      hasMore: boolean;
      replaceEntries?: boolean;
    };
    dispatch({
      kind: 'older',
      entries: page.entries,
      startByteOffset: page.startByteOffset,
      hasMore: page.hasMore,
      replace: page.replaceEntries,
    });
  }, [state.jsonlPath, state.hasMore, state.startByteOffset]);

  const tasks = useMemo(() => foldTasks(state.entries), [state.entries]);

  /** Starts the agent again on its stopped session, in the tab's terminal. */
  const resumeSession = useCallback(() => {
    const sessionId = state.sessionId ?? sessionRef.current;
    if (!sessionId) return false;
    return (
      socketRef.current?.send(
        JSON.stringify({ type: 'timeline:resume', sessionId, tmuxSession: sessionName }),
      ) ?? false
    );
  }, [state.sessionId, sessionName]);

  return {
    entries: state.entries,
    tasks,
    sessionId: state.sessionId,
    jsonlPath: state.jsonlPath,
    hasMore: state.hasMore,
    initMeta: state.initMeta,
    sessionStats: state.sessionStats,
    liveText: state.liveText,
    isLoading: state.isLoading,
    error: state.error,
    wsStatus: state.wsStatus,
    starting: state.starting,
    resume: state.resume,
    resumeSession,
    loadMore,
    retrySession: bump,
  };
}
