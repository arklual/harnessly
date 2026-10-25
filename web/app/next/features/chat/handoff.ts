// The blocks a conversation's context travels in (internal/handoff on the runtime). A handoff's first message is a
// <handoff> block: the excerpt or the note of the chat it came from, with what the agent is to do with it; a fork's
// result goes back as a <fork-result> block. Neither is the person's words: the feed draws them as a card that says
// where they come from, and the instructions to the agent are not shown.
import { createContext } from 'react';

export const BLOCK_KIND = { handoff: 'handoff', fork: 'fork', result: 'result' } as const;
export type TBlockKind = (typeof BLOCK_KIND)[keyof typeof BLOCK_KIND];

export const BLOCK_MODE = { excerpt: 'excerpt', note: 'note' } as const;
export type TBlockMode = (typeof BLOCK_MODE)[keyof typeof BLOCK_MODE];

export interface IContextBlock {
  kind: TBlockKind;
  /** The chat it came from (a handoff, a fork) or the fork that sends it (a result). */
  fromId: string;
  title: string;
  agent: 'claude' | 'codex' | null;
  model: string | null;
  /** How a handoff carries the context; null for a result. */
  mode: TBlockMode | null;
  text: string;
}

const BLOCK =
  /^\s*<(handoff|fork-result)((?:\s+[a-z]+="[^"]*")*)>\n<instructions>[\s\S]*?<\/instructions>\n\n([\s\S]*?)\n<\/\1>\s*$/;

const ENTITIES = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"' } as const;
const unescape = (value: string) =>
  value.replace(/&(?:amp|lt|gt|quot);/g, (all) => ENTITIES[all as keyof typeof ENTITIES] ?? all);

const attrs = (raw: string): Record<string, string> => {
  const out: Record<string, string> = {};
  for (const m of raw.matchAll(/([a-z]+)="([^"]*)"/g)) out[m[1]] = unescape(m[2]);
  return out;
};

const agentOf = (value: string | undefined): IContextBlock['agent'] =>
  value === 'claude' || value === 'codex' ? value : null;

const modeOf = (value: string | undefined): TBlockMode | null =>
  value === BLOCK_MODE.excerpt || value === BLOCK_MODE.note ? value : null;

const kindOf = (tag: string, a: Record<string, string>): TBlockKind => {
  if (tag === 'fork-result') return BLOCK_KIND.result;
  return a.kind === 'fork' ? BLOCK_KIND.fork : BLOCK_KIND.handoff;
};

/** The context block this message is, or null for the person's own words. */
export const parseContextBlock = (input: string): IContextBlock | null => {
  const m = BLOCK.exec(input);
  if (!m) return null;
  const a = attrs(m[2]);
  if (!a.from) return null;
  const tag = m[1];
  return {
    kind: kindOf(tag, a),
    fromId: a.from,
    title: a.title ?? '',
    agent: agentOf(a.agent),
    model: a.model ? a.model : null,
    mode: tag === 'fork-result' ? null : modeOf(a.mode),
    // The runtime breaks a closing tag inside the text so it cannot end the block early; here it reads as written.
    text: m[3].replaceAll(`<\\/${tag}`, `</${tag}`),
  };
};

/** The chats a card or a divider links to: open one here when it is on this machine. */
export interface IChatLinks {
  has: (id: string) => boolean;
  open: (id: string) => void;
}

export const ChatLinks = createContext<IChatLinks | null>(null);
