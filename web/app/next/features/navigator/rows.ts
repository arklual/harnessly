// The navigator of a chat (⌘⌥J): what a person can jump to in the open chat — their own prompts and the subagents the
// agent started — read from the conversation's stores as plain rows the search dialog draws.
import type { ISearchGroup, ISearchRow } from '@/next/features/search/search-dialog';
import { entryStore, feedStore, key, turnStore } from '@/next/stores/conversation';
import type { ConvId, Entry } from '@/next/protocol/v2';

export const NAV_ROW = { prompt: 'nav-prompt:', agent: 'nav-agent:' } as const;
const PROMPTS = 40;
const AGENTS = 20;

export interface INavSource {
  turns: readonly { id: string; entryIds: readonly string[] }[];
  entry: (id: string) => Entry | undefined;
}

const oneLine = (text: string): string => text.replace(/\s+/g, ' ').trim();

/** The prompts and subagents of a chat, newest first, as the groups of the dialog (empty groups left out). */
export const navigatorGroups = (
  source: INavSource,
  labels: { prompts: string; agents: string },
  at: (ms: number) => string,
): ISearchGroup[] => {
  const prompts: ISearchRow[] = [];
  const agents: ISearchRow[] = [];
  for (const turn of source.turns) {
    const opener = source.entry(turn.id);
    if (opener?.t === 'user' && oneLine(opener.text) !== '') {
      prompts.push({
        id: NAV_ROW.prompt + turn.id,
        kind: 'message',
        icon: 'message-square',
        title: oneLine(opener.text).slice(0, 140),
        meta: at(opener.at),
        target: { chatId: '', entryId: opener.id, turnId: turn.id },
      });
    }
    for (const id of turn.entryIds) {
      const e = source.entry(id);
      if (e?.t !== 'subagent') continue;
      agents.push({
        id: NAV_ROW.agent + e.id,
        kind: 'message',
        icon: 'bot',
        title: oneLine(e.description || e.agentType),
        meta: e.agentType,
        target: { chatId: '', entryId: e.id, turnId: turn.id },
      });
    }
  }
  return [
    { id: 'prompts', label: labels.prompts, rows: prompts.reverse().slice(0, PROMPTS) },
    { id: 'agents', label: labels.agents, rows: agents.reverse().slice(0, AGENTS) },
  ].filter((g) => g.rows.length > 0);
};

/** Reads the open chat from the stores. */
export const sourceOf = (conv: ConvId): INavSource => ({
  turns: (feedStore.get(conv)?.rows ?? []).flatMap((row) => {
    if (row.k !== 'turn') return [];
    return [{ id: row.id, entryIds: turnStore.get(key(conv, row.id))?.entryIds ?? [] }];
  }),
  entry: (id) => entryStore.get(key(conv, id)),
});

/** The groups whose rows hold every word of the query (all rows for an empty one). */
export const filterGroups = (groups: readonly ISearchGroup[], query: string): ISearchGroup[] => {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [...groups];
  return groups
    .map((g) => ({
      ...g,
      rows: g.rows.filter((r) => {
        const hay = `${r.title} ${r.meta}`.toLowerCase();
        return words.every((w) => hay.includes(w));
      }),
    }))
    .filter((g) => g.rows.length > 0);
};
