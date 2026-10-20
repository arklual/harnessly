import { isSubagentCall, type TGroupedItem } from '@/lib/timeline-grouping';
import { modelSwitchOf } from '@/lib/hl-models';
import {
  isKeptModelOutput,
  isSwitchOutput,
  slashCommandEffort,
  slashCommandModel,
} from '@/lib/hl-command-output';
import { describeToolCall, summarizeToolGroup } from '@/lib/hl-tool-label';
import { isInterruptedResult } from '@/lib/running-step';
import { formatSpan } from '@/lib/turn-format';
import { t } from '@/lib/i18n';
import type { ITimelineToolCall } from '@/types/timeline';

export interface ITurn {
  id: string;
  startedAt: number;
  endedAt: number;
  /** What folds under the summary: thoughts, tool runs, subagents. */
  work: TGroupedItem[];
  pinned: TGroupedItem[];
  /** What stays in view while the turn runs and after it: every message of the agent and the pinned rows, as they came. */
  shown: TGroupedItem[];
  /** The turn as it reads: each shown row, and between them one line for the work done in that gap. */
  flow: TTurnFlowRow[];
  final: TGroupedItem | null;
  /** The text the turn ends on: its answer, or else the last narration, kept out of `work` so it is drawn once. */
  closing: TGroupedItem | null;
  closingIsFinal: boolean;
  actions: number;
  thinking: { id: string; ms: number }[];
  interrupted: boolean;
}

export type TTurnFlowRow =
  { kind: 'shown'; id: string; item: TGroupedItem } | { kind: 'actions'; id: string; items: TGroupedItem[] };

export type TTurnRow =
  | { kind: 'user'; id: string; item: TGroupedItem }
  | { kind: 'turn'; id: string; turn: ITurn }
  | { kind: 'standalone'; id: string; item: TGroupedItem };

const MIN_THINKING_MS = 1000;

const PINNED = new Set([
  'plan',
  'ask-user-question',
  'approval-request',
  'error-notice',
  'interrupt',
  'context-compacted',
  'slash-command',
  'command-output',
]);

const entryOf = (item: TGroupedItem) => (item.type === 'entry' ? item.entry : null);

// Model and effort switches render as a divider line between turns, so they must not land inside one.
const isStandalone = (item: TGroupedItem) => {
  const entry = entryOf(item);
  if (!entry) return false;
  switch (entry.type) {
    case 'session-exit':
      return true;
    case 'user-message':
      return modelSwitchOf(entry.text) !== null;
    case 'slash-command':
      return !!slashCommandModel(entry)?.switched || slashCommandEffort(entry) !== null;
    case 'command-output':
      return isSwitchOutput(entry.output) && !isKeptModelOutput(entry.output);
    default:
      return false;
  }
};

const isUser = (item: TGroupedItem) => entryOf(item)?.type === 'user-message' && !isStandalone(item);

// Claude Code has not taken it yet: the running turn goes on, and splitting it would make the live row jump.
const isWaitingQueued = (item: TGroupedItem) => {
  const entry = entryOf(item);
  return entry?.type === 'user-message' && !!entry.queued && !!entry.pending;
};

const isPinned = (item: TGroupedItem) => {
  const entry = entryOf(item);
  return (
    !!entry &&
    (PINNED.has(entry.type) || (entry.type === 'tool-call' && (!!entry.peerMessage || !!entry.sentFiles)))
  );
};

const isText = (item: TGroupedItem) => entryOf(item)?.type === 'assistant-message';

const isNotification = (item: TGroupedItem) => entryOf(item)?.type === 'task-notification';

const timestampOf = (item: TGroupedItem) =>
  item.type === 'tool-group' ? item.toolCalls[0].timestamp : item.entry.timestamp;

const endOf = (item: TGroupedItem) =>
  item.type === 'tool-group'
    ? Math.max(...item.toolCalls.map((c) => c.timestamp), ...item.toolResults.map((r) => r.timestamp))
    : item.entry.timestamp;

const buildTurn = (id: string, startedAt: number, body: readonly TGroupedItem[]): ITurn => {
  // Text is the answer only when no work follows it; text between tool runs is narration of the work.
  // A background task reporting in after the answer does not take the answer back.
  const finalIndex = body.findLastIndex(isText);
  const lastWorkIndex = body.findLastIndex(
    (item, i) => !isPinned(item) && !isText(item) && !(i > finalIndex && isNotification(item)),
  );
  const final = finalIndex > lastWorkIndex ? body[finalIndex] : null;
  const closing = finalIndex >= 0 ? body[finalIndex] : null;
  const work: TGroupedItem[] = [];
  const pinned: TGroupedItem[] = [];
  const shown: TGroupedItem[] = [];
  const flow: TTurnFlowRow[] = [];
  const show = (item: TGroupedItem) => {
    shown.push(item);
    flow.push({ kind: 'shown', id: item.id, item });
  };
  const thinking: { id: string; ms: number }[] = [];
  let actions = 0;
  let previousAt = startedAt;
  let endedAt = startedAt;
  for (const item of body) {
    const end = endOf(item);
    endedAt = Math.max(endedAt, end);
    // Every message of the agent is read where it was written, however short, and never moves when the next arrives.
    if (isText(item)) {
      show(item);
      previousAt = end;
      continue;
    }
    if (isPinned(item)) {
      pinned.push(item);
      show(item);
    } else {
      work.push(item);
      const line = flow.at(-1);
      // Named after its first row, so a line that grows keeps its place and its open state.
      if (line?.kind === 'actions') line.items.push(item);
      else flow.push({ kind: 'actions', id: `actions:${item.id}`, items: [item] });
    }
    const entry = entryOf(item);
    if (item.type === 'tool-group') actions += item.toolCalls.length;
    else if (entry && (isSubagentCall(entry) || entry.type === 'agent-group')) actions += 1;
    // Claude Code writes a thinking block once it is done, so it took the time since the previous row ended.
    if (entry?.type === 'thinking')
      thinking.push({ id: item.id, ms: Math.max(MIN_THINKING_MS, entry.timestamp - previousAt) });
    previousAt = end;
  }
  return {
    id,
    startedAt,
    endedAt,
    work,
    pinned,
    shown,
    flow,
    final,
    closing,
    closingIsFinal: final !== null,
    actions,
    thinking,
    interrupted: pinned.some((item) => entryOf(item)?.type === 'interrupt'),
  };
};

const callsOf = (item: TGroupedItem): ITimelineToolCall[] => {
  if (item.type === 'tool-group') return item.toolCalls;
  if (item.entry.type === 'tool-call') return [item.entry];
  if (item.entry.type === 'agent-group')
    return [
      {
        id: item.id,
        type: 'tool-call',
        timestamp: item.entry.timestamp,
        toolUseId: item.id,
        toolName: 'Agent',
        summary: item.entry.description,
        status: 'success',
      },
    ];
  return [];
};

/**
 * One line for a group of actions between two messages: what was done ("Edit in timeline-turns.ts · 1 command"),
 * or on the live line what runs right now. A group of thoughts only says how long the agent thought.
 */
export const actionsLabel = (
  items: readonly TGroupedItem[],
  options: { live?: boolean; thinkingMs?: number } = {},
): string => {
  const calls = items.flatMap(callsOf);
  if (options.live) {
    const results = items.flatMap((item) => (item.type === 'tool-group' ? item.toolResults : []));
    const running = calls.findLast(
      (c) => c.status === 'pending' && !isInterruptedResult(results.find((r) => r.toolUseId === c.toolUseId)),
    );
    if (running) {
      const label = describeToolCall(running);
      return label.target ? `${label.verb} ${label.target}` : label.verb;
    }
  }
  if (calls.length) return summarizeToolGroup(calls);
  return options.thinkingMs
    ? t('lib.timelineTurns.thoughtFor', { span: formatSpan(options.thinkingMs) })
    : t('lib.timelineTurns.thought');
};

/** Splits grouped timeline items into user messages, agent turns and divider rows (model switch, session exit). */
export const deriveTurns = (items: readonly TGroupedItem[]): TTurnRow[] => {
  const rows: TTurnRow[] = [];
  let turnId = 'turn:head';
  let startedAt: number | null = null;
  let body: TGroupedItem[] = [];
  let waiting: TGroupedItem[] = [];

  const close = () => {
    if (body.length > 0)
      rows.push({
        kind: 'turn',
        id: turnId,
        turn: buildTurn(turnId, startedAt ?? timestampOf(body[0]), body),
      });
    for (const item of waiting) rows.push({ kind: 'user', id: item.id, item });
    body = [];
    waiting = [];
  };

  for (const item of items) {
    if (isWaitingQueued(item)) {
      waiting.push(item);
    } else if (isUser(item)) {
      close();
      rows.push({ kind: 'user', id: item.id, item });
      turnId = `turn:${item.id}`;
      startedAt = timestampOf(item);
    } else if (isStandalone(item)) {
      close();
      rows.push({ kind: 'standalone', id: item.id, item });
      turnId = `turn:after:${item.id}`;
      startedAt = null;
    } else {
      body.push(item);
    }
  }
  close();
  return rows;
};

/**
 * How long the agent worked on the turn: from its first work row to its end. The turn's start is the user message,
 * and a message delivered from the queue carries the time it was queued, long before the agent took it.
 */
export const turnWorkMs = (turn: ITurn): number => {
  const first = turn.work[0];
  return first ? Math.max(turn.endedAt - timestampOf(first), 0) : 0;
};

const holds = (item: TGroupedItem, entryId: string) =>
  item.id === entryId ||
  (item.type === 'tool-group' &&
    (item.toolCalls.some((c) => c.id === entryId) || item.toolResults.some((r) => r.id === entryId)));

/**
 * Where a jump target hides when its turn is folded: the turn and the work row holding the entry (a call or result
 * points at its tool run). Rows that are always drawn (messages, pinned rows, the closing text) give null.
 */
export const locateEntry = (
  rows: readonly TTurnRow[],
  entryId: string,
): { turnId: string; itemId: string } | null => {
  for (const row of rows) {
    if (row.kind !== 'turn') continue;
    const item = row.turn.work.find((w) => holds(w, entryId));
    if (item) return { turnId: row.id, itemId: item.id };
  }
  return null;
};

const emptyTurn = (id: string, at: number): ITurn => ({
  id,
  startedAt: at,
  endedAt: at,
  work: [],
  pinned: [],
  shown: [],
  flow: [],
  final: null,
  closing: null,
  closingIsFinal: false,
  actions: 0,
  thinking: [],
  interrupted: false,
});

/**
 * While the agent works on a message that has no rows yet, it still gets its turn, under the id the real one will
 * have, so the live row is not remounted when the first row lands. Messages waiting in the queue stay after it.
 */
export const withPendingTurn = (rows: TTurnRow[]): TTurnRow[] => {
  const index = rows.findLastIndex((row) => !(row.kind === 'user' && isWaitingQueued(row.item)));
  const anchor = rows[index];
  if (!anchor || anchor.kind === 'turn') return rows;
  const id = anchor.kind === 'user' ? `turn:${anchor.id}` : `turn:after:${anchor.id}`;
  return [
    ...rows.slice(0, index + 1),
    { kind: 'turn', id, turn: emptyTurn(id, timestampOf(anchor.item)) },
    ...rows.slice(index + 1),
  ];
};

const sameItems = (a: readonly TGroupedItem[], b: readonly TGroupedItem[]) =>
  a.length === b.length && a.every((item, i) => item === b[i]);

const sameTurn = (a: ITurn, b: ITurn) =>
  a.startedAt === b.startedAt &&
  a.final === b.final &&
  a.closing === b.closing &&
  sameItems(a.work, b.work) &&
  sameItems(a.shown, b.shown);

const sameRow = (a: TTurnRow, b: TTurnRow) => {
  if (a.kind === 'turn' || b.kind === 'turn')
    return a.kind === 'turn' && b.kind === 'turn' && sameTurn(a.turn, b.turn);
  return a.kind === b.kind && a.item === b.item;
};

/**
 * Derives turns and hands back the previous row objects whose items did not change, so a memoized turn row
 * re-renders only when its own content moved, like `createTimelineGrouper`.
 */
export const createTurnDeriver = () => {
  let previous = new Map<string, TTurnRow>();
  return (items: readonly TGroupedItem[]): TTurnRow[] => {
    const next = new Map<string, TTurnRow>();
    const rows = deriveTurns(items).map((row) => {
      const before = previous.get(row.id);
      const stable = before && sameRow(before, row) ? before : row;
      next.set(row.id, stable);
      return stable;
    });
    previous = next;
    return rows;
  };
};
