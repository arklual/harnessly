// The journal of events (docs/hub/event-bus.md): what the page shows of it and how it keeps the live list.

/** One event of the journal: an envelope without text. */
export interface BusEvent {
  v: number;
  seq: number;
  id: string;
  owner: string;
  org?: string;
  space?: string;
  visibility: 'person' | 'team';
  type: string;
  source: string;
  connection?: string;
  ext?: string;
  fingerprint: string;
  at: string;
  received: string;
  actor?: string;
  url?: string;
  thread?: string;
  ref?: string;
  mine?: boolean;
  facts?: Record<string, string>;
  via: string[];
  machine?: string;
}

/** A level of the journal the person may read. */
export interface BusLevel {
  scope: string;
  kind: 'me' | 'space' | 'org' | 'all';
  name: string;
}

export interface BusSubscription {
  id: string;
  owner: string;
  consumer: string;
  name?: string;
  filter: Record<string, unknown>;
  cursor: number;
  state: 'active' | 'paused' | 'failing';
  failures?: number;
  lastError?: string;
  retryAt?: string;
  created: string;
  updated: string;
}

/** What the hub does with a kind of event (the catalog of triggers). */
export type TriggerMode = 'digest' | 'notify' | 'agent' | 'off';

export const TRIGGER_MODES: readonly TriggerMode[] = ['digest', 'notify', 'agent', 'off'];

/** One kind of event of one of the person's connections, and its mode. */
export interface BusTrigger {
  source: string;
  connection?: string;
  type: string;
  mode: TriggerMode;
  seen: number;
  first: string;
  last: string;
}

/** The base of the journal's API on the hub. */
export const EVENTS_API = '/api/platform/events';

/** How many events the page keeps in memory: the oldest fall off; «Раньше» reads them again. */
export const KEEP = 2000;

/**
 * merge puts events into a list kept newest first, without repeats (a reconnect may send an event twice; the journal
 * delivers at least once), and cuts it to KEEP.
 */
export const merge = (list: readonly BusEvent[], incoming: readonly BusEvent[]): BusEvent[] => {
  if (incoming.length === 0) return list as BusEvent[];
  const seen = new Set(list.map((e) => e.seq));
  const fresh = incoming.filter((e) => !seen.has(e.seq));
  if (fresh.length === 0) return list as BusEvent[];
  return [...list, ...fresh].sort((a, b) => b.seq - a.seq).slice(0, KEEP);
};

/** The domain of a type: «chat» of «chat.message». */
export const domainOf = (type: string): string => type.split('.')[0] ?? type;

/** Whether an event matches the page's search: words of its type, source, actor, thread, facts, all of them. */
export const matches = (e: BusEvent, search: string): boolean => {
  const words = search.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return true;
  const hay = [
    e.type,
    e.source,
    e.actor,
    e.thread,
    e.connection,
    e.ext,
    ...e.via,
    ...Object.entries(e.facts ?? {}).flat(),
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();
  return words.every((w) => hay.includes(w));
};

/** The chain of an event: the events of its thread (or of its fingerprint), oldest first. */
export const chainOf = (list: readonly BusEvent[], e: BusEvent): BusEvent[] => {
  const key = e.thread
    ? (x: BusEvent) => x.thread === e.thread
    : (x: BusEvent) => x.fingerprint === e.fingerprint;
  return list.filter(key).sort((a, b) => a.seq - b.seq);
};

/** Counts of the domains in a list, the most frequent first. */
export const domains = (list: readonly BusEvent[]): [string, number][] => {
  const n = new Map<string, number>();
  for (const e of list) n.set(domainOf(e.type), (n.get(domainOf(e.type)) ?? 0) + 1);
  return [...n.entries()].sort((a, b) => b[1] - a[1]);
};

/** How far a subscription is behind the journal. */
export const lag = (s: BusSubscription, head: number): number => Math.max(0, head - s.cursor);
