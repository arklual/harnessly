import type { TGroupedItem } from '@/lib/timeline-grouping';
import type { ITurn } from '@/lib/timeline-turns';
import type { ITimelineToolCall, ITimelineToolResult } from '@/types/timeline';

/** The summary shows how long the running step takes only once it is this old. */
export const STEP_AGE_AFTER_MS = 10_000;

// Same markers as `INTERRUPT_PREFIX` in session-parser.ts; that module pulls in fs and cannot reach the client bundle.
const INTERRUPTED_PREFIX = '[Request interrupted by user';
const REJECTED_MARKER = "The user doesn't want to proceed with this tool use";

/** Claude Code closes a call it stopped on Esc with one of these results. */
export const isInterruptedResult = (result: ITimelineToolResult | undefined): boolean =>
  !!result && (result.summary.startsWith(INTERRUPTED_PREFIX) || result.summary.includes(REJECTED_MARKER));

const callsOf = (item: TGroupedItem): readonly ITimelineToolCall[] => {
  if (item.type === 'tool-group') return item.toolCalls;
  return item.entry.type === 'tool-call' ? [item.entry] : [];
};

/**
 * The tool call the turn is in right now: a pending call of its last tool run. Claude Code writes `tool_use` when the
 * call starts, so its timestamp is when the step began. A call left pending under later work never finished, and a
 * stopped turn runs nothing.
 */
export const runningStepOfTurn = (turn: ITurn): { callId: string; startedAt: number } | null => {
  if (turn.interrupted) return null;
  const last = turn.work.at(-1);
  if (!last) return null;
  const results = last.type === 'tool-group' ? last.toolResults : [];
  const call = callsOf(last).findLast(
    (c) => c.status === 'pending' && !isInterruptedResult(results.find((r) => r.toolUseId === c.toolUseId)),
  );
  return call ? { callId: call.toolUseId, startedAt: call.timestamp } : null;
};
