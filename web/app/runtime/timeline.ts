// The timeline socket's state: the conversation's entries as the runtime parses them from the agent's transcript.

export interface TimelineEntry {
  id: string;
  type: string;
  timestamp: number;
  [k: string]: unknown;
}

export interface ToolResult {
  isError: boolean;
  summary: string;
}

export interface TimelineState {
  entries: TimelineEntry[];
  /** Tool results by toolUseId; the runtime sends them as entries of their own. */
  results: Record<string, ToolResult>;
  sessionId: string;
  jsonlPath: string | null;
  hasMore: boolean;
  startByteOffset: number;
  /** The reply the agent has printed but not yet written to its transcript. */
  liveText: string;
  /** No conversation yet (the agent is starting, or the pane runs no agent). */
  waiting: boolean;
  error: string | null;
  loaded: boolean;
}

export const emptyTimeline: TimelineState = {
  entries: [],
  results: {},
  sessionId: '',
  jsonlPath: null,
  hasMore: false,
  startByteOffset: 0,
  liveText: '',
  waiting: false,
  error: null,
  loaded: false,
};

type Msg = { type?: unknown; [k: string]: unknown };

function split(entries: TimelineEntry[], results: Record<string, ToolResult>, known: Set<string>) {
  const shown: TimelineEntry[] = [];
  for (const e of entries) {
    if (e.type === 'tool-result') {
      if (typeof e.toolUseId === 'string') {
        results[e.toolUseId] = { isError: e.isError === true, summary: String(e.summary ?? '') };
      }
      continue;
    }
    if (known.has(e.id)) continue;
    known.add(e.id);
    shown.push(e);
  }
  return shown;
}

/** A tool call's state: what the call says, or what its result says once it came. */
export function callStatus(
  e: TimelineEntry,
  results: Record<string, ToolResult>,
): 'pending' | 'success' | 'error' {
  const r = typeof e.toolUseId === 'string' ? results[e.toolUseId] : undefined;
  if (r) return r.isError ? 'error' : 'success';
  return e.status === 'error' ? 'error' : e.status === 'success' ? 'success' : 'pending';
}

export function reduceTimeline(state: TimelineState, msg: Msg): TimelineState {
  switch (msg.type) {
    case 'timeline:init': {
      const results: Record<string, ToolResult> = {};
      const entries = split((msg.entries as TimelineEntry[]) ?? [], results, new Set());
      const sessionId = typeof msg.sessionId === 'string' ? msg.sessionId : '';
      return {
        ...emptyTimeline,
        entries,
        results,
        sessionId,
        jsonlPath: typeof msg.jsonlPath === 'string' ? msg.jsonlPath : null,
        hasMore: msg.hasMore === true,
        startByteOffset: typeof msg.startByteOffset === 'number' ? msg.startByteOffset : 0,
        waiting: sessionId === '',
        loaded: true,
      };
    }
    case 'timeline:append': {
      const results = { ...state.results };
      const known = new Set(state.entries.map((e) => e.id));
      const added = split((msg.entries as TimelineEntry[]) ?? [], results, known);
      return {
        ...state,
        entries: added.length ? [...state.entries, ...added] : state.entries,
        results,
        liveText: '',
      };
    }
    case 'timeline:live-text':
      return { ...state, liveText: typeof msg.text === 'string' ? msg.text : '' };
    case 'timeline:session-changed':
      return { ...state, waiting: !msg.newSessionId, liveText: '' };
    case 'timeline:error':
      return {
        ...state,
        error: typeof msg.message === 'string' ? msg.message : 'The conversation could not be read',
      };
    default:
      return state;
  }
}

/** Older entries fetched over HTTP go in front. */
export function prependEntries(
  state: TimelineState,
  page: { entries: TimelineEntry[]; startByteOffset: number; hasMore: boolean; replaceEntries?: boolean },
): TimelineState {
  const results = { ...state.results };
  if (page.replaceEntries) {
    const entries = split(page.entries, results, new Set());
    return { ...state, entries, results, startByteOffset: page.startByteOffset, hasMore: page.hasMore };
  }
  const known = new Set(state.entries.map((e) => e.id));
  const older = split(page.entries, results, known);
  return {
    ...state,
    entries: [...older, ...state.entries],
    results,
    startByteOffset: page.startByteOffset,
    hasMore: page.hasMore,
  };
}

// --- grouping for display -----------------------------------------------------------------------------------------

export type TimelineItem =
  | { kind: 'user'; entry: TimelineEntry }
  | { kind: 'assistant'; entry: TimelineEntry }
  | { kind: 'work'; id: string; entries: TimelineEntry[] }
  | { kind: 'notice'; entry: TimelineEntry };

const NOTICE = new Set([
  'interrupt',
  'error-notice',
  'context-compacted',
  'session-exit',
  'plan',
  'ask-user-question',
  'approval-request',
  'task-notification',
]);
const HIDDEN = new Set(['turn-end']);

/**
 * Messages stay as they are; everything the agent did between two messages (tool calls, thoughts, commands, sub-agents)
 * folds into one work item.
 */
export function groupTimeline(entries: TimelineEntry[]): TimelineItem[] {
  const out: TimelineItem[] = [];
  let work: TimelineEntry[] = [];
  const flush = () => {
    if (work.length && work[0]) out.push({ kind: 'work', id: work[0].id, entries: work });
    work = [];
  };
  for (const e of entries) {
    if (HIDDEN.has(e.type)) continue;
    if (e.type === 'user-message') {
      flush();
      out.push({ kind: 'user', entry: e });
    } else if (e.type === 'assistant-message') {
      flush();
      out.push({ kind: 'assistant', entry: e });
    } else if (NOTICE.has(e.type)) {
      flush();
      out.push({ kind: 'notice', entry: e });
    } else {
      work.push(e);
    }
  }
  flush();
  return out;
}

/** The one line a folded work item shows. */
export function workSummary(entries: TimelineEntry[]): string {
  let calls = 0;
  let thoughts = 0;
  let last = '';
  for (const e of entries) {
    if (e.type === 'thinking' || e.type === 'reasoning-summary') thoughts++;
    else {
      calls++;
      last = entryLabel(e);
    }
  }
  const parts: string[] = [];
  if (calls) parts.push(`${calls} ${calls === 1 ? 'step' : 'steps'}`);
  if (thoughts) parts.push(`${thoughts} ${thoughts === 1 ? 'thought' : 'thoughts'}`);
  return last ? `${parts.join(' · ')} — ${last}` : parts.join(' · ');
}

/** A short name of what an entry did. */
export function entryLabel(e: TimelineEntry): string {
  switch (e.type) {
    case 'tool-call':
      return [e.toolName, e.summary].filter(Boolean).join(' ');
    case 'exec-command-stream':
      return `$ ${String(e.command ?? '')}`;
    case 'patch-apply':
      return `Patch ${Array.isArray(e.files) ? e.files.length : 0} file(s)`;
    case 'mcp-tool-call':
      return `${String(e.server ?? '')}.${String(e.tool ?? '')}`;
    case 'web-search':
      return 'Web search';
    case 'agent-group':
      return `Sub-agent: ${String(e.description ?? e.agentType ?? '')}`;
    case 'task-progress':
      return `Task: ${String(e.subject ?? e.taskId ?? '')}`;
    case 'slash-command':
      return `/${String(e.name ?? '').replace(/^\//, '')} ${String(e.args ?? '')}`.trim();
    case 'command-output':
      return 'Command output';
    case 'thinking':
    case 'reasoning-summary':
      return 'Thinking';
    default:
      return e.type;
  }
}
