import { useEffect, useReducer, useRef } from 'react';

import { socketUrl } from '../api/base';
import { request } from '../api/http';
import { ReconnectingSocket } from '../api/socket';
import {
  emptyTimeline,
  prependEntries,
  reduceTimeline,
  type TimelineEntry,
  type TimelineState,
} from '../api/timeline';
import type { AgentPanel } from '../api/types';

type Action =
  { kind: 'msg'; msg: { type?: unknown } } | { kind: 'older'; page: Parameters<typeof prependEntries>[1] };

function reducer(state: TimelineState, a: Action): TimelineState {
  return a.kind === 'msg' ? reduceTimeline(state, a.msg) : prependEntries(state, a.page);
}

/** The conversation of an agent tab, live from the timeline socket. */
export function useTimeline(
  session: string,
  panelType: AgentPanel,
  agentSessionId: string | null | undefined,
) {
  const [state, dispatch] = useReducer(reducer, emptyTimeline);
  const sessionRef = useRef(agentSessionId);
  useEffect(() => {
    sessionRef.current = agentSessionId;
  }, [agentSessionId]);

  useEffect(() => {
    const sock = new ReconnectingSocket({
      url: () => {
        const sid = sessionRef.current;
        return socketUrl(
          `/api/timeline?session=${encodeURIComponent(session)}&panelType=${panelType}${sid ? `&agentSessionId=${encodeURIComponent(sid)}` : ''}`,
        );
      },
      onMessage: (data) => {
        if (typeof data !== 'string') return;
        try {
          dispatch({ kind: 'msg', msg: JSON.parse(data) as { type?: unknown } });
        } catch {
          // not a message of this socket
        }
      },
      // A tab that is not an agent's, or a missing session parameter, will not get better by reconnecting.
      shouldRetry: (ev) => ev.code !== 1008,
      minDelay: 1000,
    });
    const beat = setInterval(() => sock.send('{"type":"ping"}'), 25_000);
    return () => {
      clearInterval(beat);
      sock.close();
    };
  }, [session, panelType]);

  const loadOlder = async () => {
    if (!state.jsonlPath || !state.hasMore) return;
    const page = await request<{
      entries: TimelineEntry[];
      startByteOffset: number;
      hasMore: boolean;
      replaceEntries?: boolean;
    }>(
      `/api/timeline/entries?jsonlPath=${encodeURIComponent(state.jsonlPath)}&beforeByte=${state.startByteOffset}&limit=128`,
    );
    dispatch({ kind: 'older', page });
  };

  return { state, loadOlder };
}
