// The one rule by which a conversation record enters the list, whoever brings it: a live event of its machine, the
// machine's snapshot, the cluster's copy, or what the browser kept. Every write of `conversationsStore` on the page
// goes through here, so a copy that answers late never lies over what the machine said after it was asked.
//
// A record remembers where it came from. The machine's word is versioned by (epoch, rev): within one epoch the higher
// rev wins, and another epoch (a runtime that started over counts from 1 again) wins whatever the revs. The copy and
// the browser's cache carry no version the machine would recognise, so the machine's word always replaces them; the
// copy replaces the machine's word only when it knows of later activity, and the cache only fills what is missing.
//
// The agent state of a record has two ways in: the convs channel (with the record, at the record's rev) and the
// conversation's own channel (a stamp: the state, the rev it stands at, the epoch). Both count in the record's revs, so
// the record keeps the later of the two and every reader of the list, the tab and the feed sees one state. A stamp
// never moves the record's rev: the record's other fields still wait for their own word of the convs channel.

import { batch } from '@/next/core/store';
import { conversationsStore } from '@/next/stores/conversations';
import type { AgentStamp, ConvId, Conversation, MachineId } from '@/next/protocol/v2';

export const SOURCE = {
  /** An event of the machine's convs channel. */
  live: 'live',
  /** The machine's own list. */
  snapshot: 'snapshot',
  /** The cluster's copy. */
  central: 'central',
  /** What the browser kept from the last visit. */
  cache: 'cache',
} as const;
export type Source = (typeof SOURCE)[keyof typeof SOURCE];

export interface Origin {
  src: Source;
  /** The runtime's epoch the record's rev counts in; null for a source that is not the machine. */
  epoch: string | null;
  /**
   * The page drew its own look over the record (a chat of a machine that is off, shown stopped). The record is still
   * its source's for the copy, but the machine's next word replaces it at any rev: the look is not the machine's.
   */
  veiled?: true;
}

export const FROM_CENTRAL: Origin = { src: SOURCE.central, epoch: null };
export const FROM_CACHE: Origin = { src: SOURCE.cache, epoch: null };
export const fromLive = (epoch: string | null): Origin => ({ src: SOURCE.live, epoch });
export const fromSnapshot = (epoch: string | null): Origin => ({ src: SOURCE.snapshot, epoch });

// Keyed by the stored object: a record that leaves the store takes its origin with it, nothing to clean up.
const origins = new WeakMap<Conversation, Origin>();

/** Where the record came from. One that entered the store past the rule is trusted no more than the cache. */
export const originOf = (c: Conversation): Origin => origins.get(c) ?? FROM_CACHE;

const isMachine = (o: Origin): boolean => o.src === SOURCE.live || o.src === SOURCE.snapshot;

/** What the rule compares of the word that came: a whole record, or an event that carries only its rev. */
export interface Version {
  rev: number;
  lastActivityAt: number;
}

/** The rule itself: whether a word of `from` at `next` replaces the record held. */
export function supersedes(have: Conversation | undefined, next: Version, from: Origin): boolean {
  if (!have) return true;
  const held = originOf(have);
  if (isMachine(from)) {
    if (!isMachine(held) || held.veiled || held.epoch !== from.epoch) return true;
    return next.rev > have.rev;
  }
  if (from.src === SOURCE.cache) return false;
  if (isMachine(held)) return next.lastActivityAt > have.lastActivityAt;
  // The copy over itself: the answer applied is always the last one asked. Over the cache: unless the cache is newer.
  return held.src === SOURCE.central || next.lastActivityAt >= have.lastActivityAt;
}

function store(c: Conversation, from: Origin): Conversation {
  origins.set(c, from);
  conversationsStore.set(c.id, c);
  return c;
}

// The stamp a stored record's state came by, while it is later than the record's own rev. Keyed by the stored object,
// like the origins.
const stamps = new WeakMap<Conversation, AgentStamp>();
// The stamps of chats the list does not hold yet (a feed opened before the list came): the record takes its own when
// it enters.
const early = new Map<ConvId, AgentStamp>();

/** The version the record's agent state stands at: its stamp's, or the record's own. */
export function stateVersionOf(c: Conversation): { epoch: string | null; rev: number } {
  const stamp = stamps.get(c);
  return stamp ? { epoch: stamp.epoch, rev: stamp.rev } : { epoch: originOf(c).epoch, rev: c.rev };
}

/** A record of the machine at `next.rev` keeps the state of a stamp that is later in the same epoch. */
function withLaterState(next: Conversation, stamp: AgentStamp | undefined, from: Origin): Conversation {
  if (!stamp || !isMachine(from) || stamp.epoch !== from.epoch || stamp.rev <= next.rev) return next;
  const kept = { ...next, state: stamp.state };
  stamps.set(kept, stamp);
  return kept;
}

/** Brings a whole record. Returns the record the list holds afterwards: this one, or the one that stood. */
export function mergeConversation(next: Conversation, from: Origin): Conversation {
  const have = conversationsStore.get(next.id);
  if (have && !supersedes(have, next, from)) return have;
  const stamp = have ? stamps.get(have) : early.get(next.id);
  early.delete(next.id);
  return store(withLaterState(readNoLess(next, have), stamp, from), from);
}

/**
 * What was read stays read: a word that knows of an earlier mark (the copy that has not heard of it yet, a snapshot
 * asked before it) takes the record's other fields, never its `seenAt` back. Only a later turn makes the chat unread.
 */
function readNoLess(next: Conversation, have: Conversation | undefined): Conversation {
  return have && have.seenAt > next.seenAt ? { ...next, seenAt: have.seenAt } : next;
}

/**
 * Brings the agent state of the conversation's own channel. The rule is the record's: in the epoch the state stands
 * in, only a later rev replaces it; a state that is not the machine's (the copy's, the cache's, the page's own look)
 * or of another epoch gives way at any rev. The caller has checked that the stamp is of the machine's present epoch.
 */
export function mergeAgentState(id: ConvId, stamp: AgentStamp): void {
  const have = conversationsStore.get(id);
  if (!have) {
    const held = early.get(id);
    if (!held || held.epoch !== stamp.epoch || stamp.rev > held.rev) early.set(id, stamp);
    return;
  }
  const origin = originOf(have);
  const prior = stamps.get(have);
  const machines = prior !== undefined || (isMachine(origin) && !origin.veiled);
  const version = stateVersionOf(have);
  if (machines && version.epoch === stamp.epoch && stamp.rev <= version.rev) return;
  const next = { ...have, state: stamp.state };
  stamps.set(next, stamp);
  store(next, origin);
}

/** Brings an event that changes some fields of a record at a rev; a chat the list does not hold stays absent. */
export function mergeConversationFields(
  id: ConvId,
  rev: number,
  from: Origin,
  fields: (have: Conversation) => Partial<Conversation>,
): void {
  const have = conversationsStore.get(id);
  if (!have || !supersedes(have, { rev, lastActivityAt: have.lastActivityAt }, from)) return;
  store(withLaterState(readNoLess({ ...have, ...fields(have), rev }, have), stamps.get(have), from), from);
}

/**
 * Changes a record by a fact that has no version of its own (one from outside the convs channel). The record keeps
 * its origin; `fn` returns the record itself to change nothing.
 */
export function amendConversation(id: ConvId, fn: (have: Conversation) => Conversation): void {
  const have = conversationsStore.get(id);
  if (!have) return;
  const next = fn(have);
  if (next === have) return;
  const stamp = stamps.get(have);
  if (stamp && next.state === have.state) stamps.set(next, stamp);
  store(next, originOf(have));
}

/** Draws the page's own look over a record (see `Origin.veiled`); `look` returns the record itself if it has it. */
export function veilConversation(id: ConvId, look: (have: Conversation) => Conversation): void {
  const have = conversationsStore.get(id);
  if (!have) return;
  const held = originOf(have);
  const next = look(have);
  if (next === have && held.veiled) return;
  store(next, { ...held, veiled: true });
}

export function removeConversation(id: ConvId): void {
  early.delete(id);
  conversationsStore.delete(id);
}

/**
 * The machine's own list, merged in one update of the store: its records come first, and only then the chats of the
 * machine it no longer names go. No reader ever sees the machine's chats gone and not yet back.
 */
export function mergeMachineList(
  machine: MachineId,
  list: readonly Conversation[],
  put: (c: Conversation) => void,
): void {
  batch(() => {
    for (const c of list) put(c);
    const named = new Set(list.map((c) => c.id));
    for (const c of [...conversationsStore.values()]) {
      if (c.machineId === machine && !named.has(c.id)) removeConversation(c.id);
    }
  });
}
