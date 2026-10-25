// Messages between agents in the feed (internal/peers on the runtime). A message from another agent reaches the
// conversation through its outbox, typed into the agent as a <peer-message> envelope; Claude Code's own cross-session
// messages come as <cross-session-message>. Neither is the person's words: the feed draws them as another agent's card.
// A message our agent sends is a tool step: Claude's SendMessage, or `harnessly peers send` in the shell.
import { createContext } from 'react';

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
const ENVELOPE = /^\s*<peer-message\b([^>]*)>\n?([\s\S]*?)\n?<\/peer-message>\s*$/;
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

/**
 * The message of another agent this text is, or null for the person's own words. The envelope's attributes are the
 * runtime's id, person, machine, chat, ref and via; the answer is built from `ref` alone, so an envelope of an older
 * runtime that also carries `reply="…"` reads the same as one without it.
 */
export const parsePeer = (input: string): IPeerMessage | null => {
  const pasted = PASTED.exec(input);
  const text = pasted ? pasted[2] : input;
  const envelope = ENVELOPE.exec(text);
  if (envelope) {
    const a = attrs(envelope[1]);
    const ref = a.ref && REF.test(a.ref) ? a.ref : null;
    return {
      kind: PEER_KIND.fleet,
      id: a.id ?? null,
      person: a.person ?? a.machine ?? '',
      machine: a.machine ?? null,
      chat: a.chat ?? null,
      ref,
      byPerson: a.via === 'person',
      text: envelope[2].replace(NEUTRALIZED_TAG, '<$1peer-message').trim(),
    };
  }
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

export type TPeerOutcome = { ok: true; to: string | null } | { ok: false; reason: TPeerReason | null };

/** A message our agent sent to another agent, read off its tool step. */
export interface IPeerOut {
  to: string;
  text: string;
  /** null while the step runs (or when it did not say). */
  outcome: TPeerOutcome | null;
}

// The deployment may ship the CLI under its own name: any command word before `peers send`.
const CLI_SEND = /^\$\s*[\w.-]+\s+peers?\s+send\s+(\S+)\s*([\s\S]*)$/;
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
  if (delivered) outcome = { ok: true, to: delivered[1] };
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
