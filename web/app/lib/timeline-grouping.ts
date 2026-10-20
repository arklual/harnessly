// Folds a timeline into what the conversation shows: plain entries, and runs of tool calls with their results
// as one group. Tool results never stand alone; subagent calls, agent groups and calls that carry a message or
// files for the person stay entries of their own.
import type { ITimelineEntry, ITimelineToolCall, ITimelineToolResult } from '@/types/timeline';

export type TGroupedItem =
  | { type: 'entry'; id: string; entry: ITimelineEntry }
  | { type: 'tool-group'; id: string; toolCalls: ITimelineToolCall[]; toolResults: ITimelineToolResult[] };

const SUBAGENT_TOOLS = new Set(['Task', 'Agent']);

export const isSubagentCall = (entry: ITimelineEntry): entry is ITimelineToolCall =>
  entry.type === 'tool-call' && SUBAGENT_TOOLS.has(entry.toolName);

const standsAlone = (call: ITimelineToolCall) =>
  SUBAGENT_TOOLS.has(call.toolName) || !!call.peerMessage || !!call.sentFiles;

/** Entries that do not interrupt a run of tool calls (they are not shown between them). */
const TRANSPARENT = new Set<ITimelineEntry['type']>(['tool-result', 'turn-end']);

export const groupTimelineItems = (entries: readonly ITimelineEntry[]): TGroupedItem[] => {
  const results = new Map<string, ITimelineToolResult>();
  for (const e of entries) if (e.type === 'tool-result') results.set(e.toolUseId, e);
  const out: TGroupedItem[] = [];
  let group: Extract<TGroupedItem, { type: 'tool-group' }> | null = null;
  for (const entry of entries) {
    if (entry.type === 'tool-call' && !standsAlone(entry)) {
      if (!group) {
        group = { type: 'tool-group', id: `group-${entry.id}`, toolCalls: [], toolResults: [] };
        out.push(group);
      }
      group.toolCalls.push(entry);
      const result = results.get(entry.toolUseId);
      if (result) group.toolResults.push(result);
      continue;
    }
    if (TRANSPARENT.has(entry.type)) {
      if (entry.type === 'turn-end') group = null;
      continue;
    }
    group = null;
    out.push({ type: 'entry', id: entry.id, entry });
  }
  return out;
};

/** A call's state from its result: pending until the result arrives. */
export const toolStatusOf = (
  call: ITimelineToolCall,
  results: readonly ITimelineToolResult[],
): 'pending' | 'success' | 'error' => {
  const result = results.find((r) => r.toolUseId === call.toolUseId);
  if (!result) return call.status ?? 'pending';
  return result.isError ? 'error' : 'success';
};
