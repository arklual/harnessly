// What the voice knows of the screen when a call starts: what is open beside the chat, what is selected in
// it, the chat's last lines and the agent's last answer. It goes into the call's first items (realtime.Primer.Screen)
// and only into the person's own call; the runtime journals its size, never its words.
import { ItemsBuilder } from '@/next/bindings/feed-items';
import { chatLines } from '@/next/features/app-views/host-handlers';
import type { TTab } from '@/next/features/pane/pane-tabs';
import type { ConvId, MachineId } from '@/next/protocol/v2';

/** The chat's lines the voice gets, the latest; each clipped. */
const LINES = 6;
const LINE_MAX = 300;
const ANSWER_MAX = 900;
const SELECTION_MAX = 800;

const clip = (s: string, n: number): string => {
  const t = s.replace(/\s+/g, ' ').trim();
  return t.length > n ? `${t.slice(0, n)}…` : t;
};

/** One line about the tab in front of the pane, or null when none is. */
export const paneLine = (tab: TTab | undefined): string | null => {
  if (!tab) return null;
  switch (tab.kind) {
    case 'files':
      return tab.file ? `the file ${tab.path}` : `the folder ${tab.path}`;
    case 'app':
      return tab.object ? `the app «${tab.app}», its «${tab.object}»` : `the app «${tab.app}»`;
    case 'designly':
      return tab.design ? `the design ${tab.design}` : 'the designs';
    case 'browser': {
      const at = tab.hist[tab.hi];
      return at ? `a web page ${at.title ?? at.text}` : 'a web page';
    }
    case 'shell':
      return 'a terminal';
    case 'plan':
      return "the agent's plan";
    case 'subagent':
      return "the agent's helpers";
    default:
      return null;
  }
};

export interface IScreenInput {
  /** The tab in front of the pane, when the pane is open. */
  tab?: TTab;
  /** What the person selected in it. */
  selection?: string;
  /** The host's own words about the place (a design's open page and picked blocks). */
  host?: string;
  lines: readonly { role: string; text: string }[];
}

/** The screen as the voice reads it: short lines, the newest chat last. */
export const screenText = ({ tab, selection, host, lines }: IScreenInput): string => {
  const out: string[] = [];
  const pane = paneLine(tab);
  if (pane) out.push(`Open beside the chat: ${pane}.`);
  if (host?.trim()) out.push(clip(host, 600));
  if (selection?.trim()) out.push(`Selected there: «${clip(selection, SELECTION_MAX)}»`);
  const last = lines[lines.length - 1];
  const chat = lines.slice(0, last?.role === 'assistant' ? -1 : undefined);
  if (chat.length > 0) {
    out.push('The chat so far (latest last):');
    for (const l of chat) out.push(`- ${l.role === 'user' ? 'Person' : 'Agent'}: ${clip(l.text, LINE_MAX)}`);
  }
  if (last?.role === 'assistant') out.push(`The agent's last answer: ${clip(last.text, ANSWER_MAX)}`);
  return out.join('\n');
};

/** The chat's last lines, from its feed. */
export const recentLines = (conv: ConvId, machine: MachineId) =>
  chatLines(new ItemsBuilder(conv, machine).snapshot(), LINES);
