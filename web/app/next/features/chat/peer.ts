// Messages between agents in the feed (internal/peers on the runtime). A message from another agent reaches the
// conversation through its outbox, typed into the agent as a <peer-message> envelope; Claude Code's own cross-session
// messages come as <cross-session-message>. Neither is the person's words: the feed draws them as another agent's card.
// A message our agent sends is a tool step: Claude's SendMessage, or `harnessly peers send` in the shell.
import { createContext } from 'react';
import { t } from '@/next/i18n';

/** The runtime's refusals (internal/peers/policy.go), each a key of next.peers.reason.*. */
export const PEER_REASONS = [
  'off',
  'blocked',
  'not-allowed',
  'unverified',
  'unknown-chat',
  'agent-gone',
  'empty',
  'too-long',
  'rate-limited',
  'no-relay',
  'unreachable',
  'bad-ref',
  'failed',
  'rejected',
  'expired',
  'revoked',
] as const;
export type TPeerReason = (typeof PEER_REASONS)[number];

export const PEER_KIND = {
  /** Through the runtime (harnessly peers): a machine, an owner, a chat to answer. */
  fleet: 'fleet',
  /** Claude Code's own message from another session of this machine. */
  session: 'session',
  /** A background agent of this session writing to it. */
  subagent: 'subagent',
} as const;
export type TPeerKind = (typeof PEER_KIND)[keyof typeof PEER_KIND];

export interface IPeerMessage {
  kind: TPeerKind;
  id: string | null;
  /** The sender's owner (Иван), or the session's name. */
  person: string;
  machine: string | null;
  /** The sender's chat title. */
  chat: string | null;
  /** Where an answer goes: `<machine>/<conversation id>`; null when it cannot be answered from here. */
  ref: string | null;
  /** The owner typed it in the card, not their agent. */
  byPerson: boolean;
  text: string;
}

const PASTED = /^\s*<pasted_content id="([^"]+)">\n?([\s\S]*?)\n?<\/pasted_content id="\1">\s*$/;
/** One envelope. A body cannot hold the closing tag (the runtime neutralizes it), so the shortest match is all of it. */
const ENVELOPE = /<peer-message\b([^>]*)>\n?([\s\S]*?)\n?<\/peer-message>/g;
const SESSION = /^\s*<cross-session-message\b([^>]*)>\n?([\s\S]*?)\n?<\/cross-session-message>\s*$/;
const SUBAGENT = /^\s*<agent-message\b([^>]*)>\n?([\s\S]*?)\n?<\/agent-message>\s*$/;
/** A tag of the envelope inside the text, which the runtime marked with a zero-width space (internal/peers.Wrap). */
const NEUTRALIZED_TAG = /<(\/?)peer-message\u200b/g;
const REF = /^[a-z][a-z0-9-]{0,63}\/tab-[A-Za-z0-9_-]+$/;

const ENTITIES = {
  '&amp;': '&',
  '&lt;': '<',
  '&gt;': '>',
  '&quot;': '"',
  '&#34;': '"',
  '&#39;': "'",
} as const;
const unescape = (value: string) =>
  value.replace(/&(?:amp|lt|gt|quot|#34|#39);/g, (all) => ENTITIES[all as keyof typeof ENTITIES] ?? all);

const attrs = (raw: string): Record<string, string> => {
  const out: Record<string, string> = {};
  for (const m of raw.matchAll(/([a-z-]+)="([^"]*)"/g)) out[m[1]] = unescape(m[2]);
  return out;
};

/** The envelopes a text is made of, in order; none when it holds anything but envelopes. */
const envelopes = (text: string): IPeerMessage[] => {
  const out: IPeerMessage[] = [];
  let at = 0;
  for (const m of text.matchAll(ENVELOPE)) {
    const from = m.index ?? 0;
    if (text.slice(at, from).trim() !== '') return [];
    at = from + m[0].length;
    const a = attrs(m[1]);
    out.push({
      kind: PEER_KIND.fleet,
      id: a.id ?? null,
      person: a.person ?? a.machine ?? '',
      machine: a.machine ?? null,
      chat: a.chat ?? null,
      ref: a.ref && REF.test(a.ref) ? a.ref : null,
      byPerson: a.via === 'person',
      text: m[2].replace(NEUTRALIZED_TAG, '<$1peer-message').trim(),
    });
  }
  return text.slice(at).trim() === '' ? out : [];
};

/**
 * Every message of another agent this text is: one, or the several that waited in the line and reached the agent as one
 * message (each keeps its own sender). Empty for the person's own words.
 */
export const parsePeers = (input: string): IPeerMessage[] => {
  const pasted = PASTED.exec(input);
  const fleet = envelopes(pasted ? pasted[2] : input);
  if (fleet.length > 0) return fleet;
  const one = parsePeer(input);
  return one ? [one] : [];
};

/**
 * The message of another agent this text is, or null for the person's own words. The envelope's attributes are the
 * runtime's id, person, machine, chat, ref and via; the answer is built from `ref` alone, so an envelope of an older
 * runtime that also carries `reply="…"` reads the same as one without it.
 */
export const parsePeer = (input: string): IPeerMessage | null => {
  const pasted = PASTED.exec(input);
  const text = pasted ? pasted[2] : input;
  const fleet = envelopes(text);
  // Several agents' messages typed as one read here as the first of them with the words of all (parsePeers has each).
  if (fleet.length > 0) return { ...fleet[0], text: fleet.map((one) => one.text).join('\n\n') };
  const session = SESSION.exec(text);
  if (session) {
    const a = attrs(session[1]);
    return {
      kind: PEER_KIND.session,
      id: null,
      person: a['from-name'] ?? a.from ?? '',
      machine: null,
      chat: null,
      ref: null,
      byPerson: false,
      text: session[2].trim(),
    };
  }
  const sub = SUBAGENT.exec(text);
  if (sub) {
    const a = attrs(sub[1]);
    return {
      kind: PEER_KIND.subagent,
      id: null,
      person: a.from ?? '',
      machine: null,
      chat: null,
      ref: null,
      byPerson: false,
      text: sub[2].trim(),
    };
  }
  return null;
};

/** The conversation id of a ref, null for anything that is not one. */
export const convOfRef = (ref: string): `tab-${string}` | null => {
  if (!REF.test(ref)) return null;
  return ref.slice(ref.indexOf('/') + 1) as `tab-${string}`;
};

/** held: it waits for the other machine's owner to accept it (GitHub #38). */
export type TPeerOutcome =
  { ok: true; to: string | null; held?: boolean } | { ok: false; reason: TPeerReason | null };

/** A message our agent sent to another agent, read off its tool step. */
export interface IPeerOut {
  to: string;
  text: string;
  /** null while the step runs (or when it did not say). */
  outcome: TPeerOutcome | null;
}

// The deployment may ship the CLI under its own name: any command word before `peers send`.
const CLI_SEND = /^\$\s*[\w.-]+\s+peers?\s+send\s+(?:--(?:ttl|id)\s+\S+\s+)*(\S+)\s*([\s\S]*)$/;
const HELD = /^held → (\S+)/m;
const DELIVERED = /^delivered → (.+)$/m;
const REFUSED = /refused: ([a-z-]+)/;

const quoted = (raw: string) => raw.trim().replace(/^(['"])([\s\S]*)\1$/, '$2');

const reasonOf = (raw: string | undefined): TPeerReason | null =>
  PEER_REASONS.find((reason) => reason === raw) ?? null;

/** The outgoing message of a tool step, or null for any other step. */
export const parsePeerOut = (
  tool: string,
  summary: string,
  ok: boolean | null,
  output: string | null,
): IPeerOut | null => {
  if (tool === 'SendMessage') {
    const cut = summary.indexOf('\n');
    if (cut < 0) return null;
    let outcome: TPeerOutcome | null = null;
    if (ok === true) outcome = { ok: true, to: null };
    else if (ok === false) outcome = { ok: false, reason: null };
    return { to: summary.slice(0, cut), text: summary.slice(cut + 1), outcome };
  }
  if (!/^(bash|shell|exec)/i.test(tool)) return null;
  const m = CLI_SEND.exec(summary);
  if (!m) return null;
  const text = quoted(m[2]);
  let outcome: TPeerOutcome | null = null;
  const delivered = output ? DELIVERED.exec(output) : null;
  const refusedMatch = output ? REFUSED.exec(output) : null;
  const held = output ? HELD.exec(output) : null;
  if (delivered) outcome = { ok: true, to: delivered[1] };
  else if (held) outcome = { ok: true, to: held[1], held: true };
  else if (refusedMatch) outcome = { ok: false, reason: reasonOf(refusedMatch[1]) };
  else if (ok === false) outcome = { ok: false, reason: null };
  return { to: m[1], text: text === '-' ? '' : text, outcome };
};

/** What the feed's cards may do: the binding of the open conversation provides it; without it they only show. */
export interface IPeerActions {
  /** The id of the chat a ref opens on this page, or null when it is not one of the person's. */
  chatOf: (ref: string) => string | null;
  onOpen: (conv: string) => void;
  /** Sends the person's answer to the ref through the same channel; resolves with what came of it. */
  onReply: (ref: string, text: string) => Promise<TPeerOutcome>;
}

export const PeerActions = createContext<IPeerActions | null>(null);

/** «От «<chat>»: <text>» as the runtime gives another agent's message outside the chat. */
const FROM_LINE = /^От «([^»]+)»: ([\s\S]*)$/;

export interface IPeerFrom {
  /** The sender's chat (or its owner); null for the person's own words. */
  from: string | null;
  text: string;
}

/**
 * Who wrote a message as a line outside the chat shows it (the ⌘K side, a search hit, a tab's snippet): another
 * chat with its words without the envelope, or the person. Takes both the raw envelope (an older runtime) and the
 * runtime's «От «<chat>»: <text>».
 */
export const peerFrom = (input: string): IPeerFrom => {
  const peer = parsePeer(input);
  if (peer) return { from: peer.chat || peer.person || null, text: peer.text };
  const line = FROM_LINE.exec(input);
  return line ? { from: line[1] ?? null, text: line[2] ?? '' } : { from: null, text: input };
};

/** A message as one line outside the chat: «От «<chat>»: <text>» for another agent's, as it is for the person's. */
export const peerLine = (input: string): string => {
  const said = peerFrom(input);
  return said.from === null ? said.text : `${t('next.search.from', { chat: said.from })}: ${said.text}`;
};
