import type { ITimelineEntry } from '@/types/timeline';

/**
 * Keys built from what the page already receives over WebSockets. A key changes exactly when a polled list
 * is worth refetching, so the timers behind those lists can stay slow.
 */

interface ITabActivity {
  tabName?: string;
  agentSessionId?: string | null;
  agentSummary?: string | null;
  lastUserMessage?: string | null;
}

/**
 * The conversation list shows each tab's title and orders rows by the last prompt; busy/idle flips change neither.
 * Changes when a tab appears or goes away, gets a new prompt, summary, session or name.
 */
export const agentActivityKey = (tabs: Record<string, ITabActivity>): string =>
  Object.entries(tabs)
    .map(([id, tab]) =>
      [
        id,
        tab.tabName ?? '',
        tab.agentSessionId ?? '',
        tab.agentSummary ?? '',
        tab.lastUserMessage ?? '',
      ].join('\u0000'),
    )
    .sort()
    .join('\u0001');

const SUBAGENT_TOOLS = new Set(['Agent', 'Task']);

/** Changes when the main transcript launches a subagent, gets its result, or hears from a background one. */
export const subagentActivityKey = (entries: readonly ITimelineEntry[]): string => {
  const launched = new Set<string>();
  let results = 0;
  let notices = 0;
  for (const entry of entries) {
    if (entry.type === 'tool-call' && SUBAGENT_TOOLS.has(entry.toolName)) launched.add(entry.toolUseId);
    else if (entry.type === 'tool-result' && launched.has(entry.toolUseId)) results += 1;
    else if (entry.type === 'task-notification') notices += 1;
  }
  return `${launched.size}:${results}:${notices}`;
};
