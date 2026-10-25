// One open conversation: its feed (turns and their entries), «Сейчас», the gates (questions and permissions), the
// queue and the background work. Each part is its own keyed store, so a `now` at 2 Hz does not touch the feed and a
// step re-renders the turn it lands in, not the other rows.
//
// Entries and turns are keyed by `${convId} ${id}`: a conversation id is unique in a person's fleet (a tab id), so
// the stores need no machine. Every write here is idempotent by id and rev — an event already reflected changes
// nothing and keeps the objects as they were, so rows do not remount on a replayed or re-sent snapshot.

import { createKeyedStore, inBatch, onBatchEnd, sameItems, useKey, useSlice } from '@/next/core/store';
import type { Cursor } from '@/next/core/realtime/client';
import type {
  BackgroundTask,
  Picture,
  Shot,
  ClientId,
  ConvId,
  ConvSnapshot,
  Entry,
  EntryId,
  GateState,
  MachineId,
  Ms,
  Now,
  Permission,
  Question,
  Queue,
  Turn,
  TurnCounts,
  TurnId,
} from '@/next/protocol/v2';

/** A row of the feed: a turn (folded or not) or an entry outside any turn (a notice). */
export type FeedRow = { k: 'turn'; id: TurnId; at: Ms } | { k: 'entry'; id: EntryId; at: Ms };

export interface FeedView {
  convId: ConvId;
  machineId: MachineId;
  /** false: nothing of it on the page yet (show a skeleton); true: rows are what the page has, maybe from cache. */
  loaded: boolean;
  /** live: the channel's snapshot of this connection has arrived; cached: drawn from memory or IndexedDB before it. */
  source: 'cached' | 'live';
  rows: FeedRow[];
  beforeTurnId: TurnId | null;
  /** Older pages were merged in: a later snapshot keeps their cursor. */
  older: boolean;
  /** When the page subscribed: only entries newer than this animate in. */
  subscribedAt: Ms | null;
  error: 'not-found' | null;
}

export interface TurnView {
  /** null: an entry of the turn arrived before the turn (it fills in with turn_started or the next snapshot). */
  turn: Turn | null;
  entryIds: EntryId[];
}

export interface GatesView {
  questions: Question[];
  permissions: Permission[];
}

const EMPTY_GATES: GatesView = { questions: [], permissions: [] };

export const feedStore = createKeyedStore<ConvId, FeedView>();
export const turnStore = createKeyedStore<string, TurnView>();
export const entryStore = createKeyedStore<string, Entry>();
export const nowStore = createKeyedStore<ConvId, Now>();
export const gatesStore = createKeyedStore<ConvId, GatesView>();
export const queueStore = createKeyedStore<ConvId, Queue>();
export const backgroundStore = createKeyedStore<ConvId, BackgroundTask[]>();
export const shotsStore = createKeyedStore<ConvId, Shot[]>();
/** The pictures the conversation's messages name (ConvSnapshot.pictures). */
export const picturesStore = createKeyedStore<ConvId, Picture[]>();
/**
 * The client ids of the person's messages the feed already holds as entries (a turn's prompt or a steered message
 * inside it): such a message stands in the feed and nowhere else, so the stack over the field and the bubbles of
 * messages on their way never draw it a second time.
 */
export const fedStore = createKeyedStore<ConvId, ReadonlySet<ClientId>>();
export const useFedClients = (conv: ConvId | null): ReadonlySet<ClientId> =>
  useKey(fedStore, conv) ?? NO_CLIENTS;
const NO_CLIENTS: ReadonlySet<ClientId> = new Set();

export const key = (conv: ConvId, id: string): string => `${conv} ${id}`;
const isLive = (id: EntryId) => id.startsWith('live:');

// ---- reads

export const useFeed = (conv: ConvId | null) => useKey(feedStore, conv);
export const useTurn = (conv: ConvId, turn: TurnId) => useKey(turnStore, key(conv, turn));
export const useEntry = (conv: ConvId, entry: EntryId) => useKey(entryStore, key(conv, entry));
export const useNow = (conv: ConvId | null) => useKey(nowStore, conv) ?? null;
export const useQueue = (conv: ConvId | null) => useKey(queueStore, conv) ?? null;
export const useBackground = (conv: ConvId | null): BackgroundTask[] => useKey(backgroundStore, conv) ?? NONE;
export const useGates = (conv: ConvId | null): GatesView => useKey(gatesStore, conv) ?? EMPTY_GATES;
/** The pictures the agent looked at, newest first; null until the conversation's snapshot has come. */
export const useShots = (conv: ConvId | null): Shot[] | null => useKey(shotsStore, conv) ?? null;
export const usePictures = (conv: ConvId | null): Picture[] | null => useKey(picturesStore, conv) ?? null;
const NONE: BackgroundTask[] = [];

/** The rows of the feed only: re-renders when a row comes or goes, not when a turn's insides change. */
export const useFeedRows = (conv: ConvId | null): FeedRow[] | null =>
  useSlice(
    feedStore,
    conv,
    (f) => (f?.loaded ? f.rows : null),
    (a, b) => a === b || (!!a && !!b && sameItems(a, b)),
  );

/** The open gates of a conversation: what waits for the person above the composer. */
export function openGates(g: GatesView): { questions: Question[]; permissions: Permission[] } {
  const open = (s: GateState) => s.k === 'open' || s.k === 'answering';
  return {
    questions: g.questions.filter((q) => open(q.state)),
    permissions: g.permissions.filter((p) => open(p.state)),
  };
}

/** A gate answered from another device: «Вы ответили с другого устройства». */
export const answeredElsewhere = (s: GateState, device: string): boolean =>
  s.k === 'answered' && s.byDevice !== device;

// ---- writes (the realtime layer and the pages call these inside batch())

export function ensureFeed(conv: ConvId, machine: MachineId): FeedView {
  const have = feedStore.get(conv);
  if (have) return have;
  const fresh: FeedView = {
    convId: conv,
    machineId: machine,
    loaded: false,
    source: 'cached',
    rows: [],
    beforeTurnId: null,
    older: false,
    subscribedAt: null,
    error: null,
  };
  feedStore.set(conv, fresh);
  return fresh;
}

function patchFeed(conv: ConvId, fn: (f: FeedView) => FeedView): void {
  feedStore.patch(conv, (f) => (f ? fn(f) : f));
}

/** Puts a row at its place by time; rows come almost always at the end. */
function insertRow(rows: FeedRow[], row: FeedRow): FeedRow[] {
  if (rows.some((r) => r.k === row.k && r.id === row.id)) return rows;
  let i = rows.length;
  while (i > 0 && rows[i - 1].at > row.at) i -= 1;
  return [...rows.slice(0, i), row, ...rows.slice(i)];
}

function mergeRows(have: FeedRow[], incoming: FeedRow[], drop: (r: FeedRow) => boolean): FeedRow[] {
  const seen = new Set<string>();
  const out: FeedRow[] = [];
  for (const r of [...have.filter((r) => !drop(r)), ...incoming]) {
    const k = `${r.k} ${r.id}`;
    if (seen.has(k)) continue;
    seen.add(k);
    out.push(r);
  }
  // Stable by time: rows of one moment keep the order they came in.
  const sorted = out
    .map((r, i) => [r, i] as const)
    .sort((a, b) => a[0].at - b[0].at || a[1] - b[1])
    .map(([r]) => r);
  return sameItems(sorted, have) ? have : sorted;
}

/** Writes an entry unless the page already has it at this rev or later. */
function putEntry(conv: ConvId, e: Entry): boolean {
  const k = key(conv, e.id);
  const have = entryStore.get(k);
  if (have && have.rev >= e.rev) return false;
  entryStore.set(k, e);
  if (e.t === 'user' && e.clientId) {
    const id = e.clientId;
    fedStore.patch(conv, (set) => (set?.has(id) ? set : new Set([...(set ?? []), id])));
  }
  return true;
}

function putTurn(conv: ConvId, t: Turn): void {
  turnStore.patch(key(conv, t.id), (v) => {
    if (!v) return { turn: t, entryIds: [] };
    if (v.turn && same(v.turn, t)) return v;
    return { ...v, turn: t };
  });
}

/** Records without a rev (turns, gates, tasks) are compared whole: a resent one changes nothing. */
const same = (a: unknown, b: unknown): boolean => a === b || JSON.stringify(a) === JSON.stringify(b);

/** Puts x in place of the item with its id, or at the end; the same list when nothing changes. */
function upsertById<T extends { id: string }>(list: T[], x: T): T[] {
  const i = list.findIndex((y) => y.id === x.id);
  if (i < 0) return [...list, x];
  if (same(list[i], x)) return list;
  return list.map((y, j) => (j === i ? x : y));
}

// Steps of a turn arrive in bursts (a reconnect replays hundreds): inside a batch they gather here and land in the
// turn once, as the batch ends, instead of copying the turn's entry list on every step.
const appends = new Map<string, { ids: EntryId[]; seen: Set<EntryId> }>();

function commitAppends(): void {
  if (appends.size === 0) return;
  for (const [k, a] of appends) {
    const v = turnStore.get(k);
    if (v) turnStore.set(k, { ...v, entryIds: [...v.entryIds, ...a.ids] });
  }
  appends.clear();
}
onBatchEnd(commitAppends);

function addToTurn(conv: ConvId, turnId: TurnId, entryId: EntryId, at: Ms): void {
  const k = key(conv, turnId);
  const v = turnStore.get(k);
  if (!v) {
    turnStore.set(k, { turn: null, entryIds: [entryId] });
    patchFeed(conv, (f) => ({ ...f, rows: insertRow(f.rows, { k: 'turn', id: turnId, at }) }));
    return;
  }
  let a = appends.get(k);
  if (!a) {
    if (v.entryIds.includes(entryId)) return;
    if (!inBatch()) {
      turnStore.set(k, { ...v, entryIds: [...v.entryIds, entryId] });
      return;
    }
    a = { ids: [], seen: new Set(v.entryIds) };
    appends.set(k, a);
  }
  if (a.seen.has(entryId)) return;
  a.seen.add(entryId);
  a.ids.push(entryId);
}

/** A new entry (step): into its turn, or as a row of its own outside any turn. */
export function applyStep(conv: ConvId, e: Entry, counts: TurnCounts | null): void {
  putEntry(conv, e);
  if (e.turnId) {
    addToTurn(conv, e.turnId, e.id, e.at);
    if (counts) {
      turnStore.patch(key(conv, e.turnId), (v) =>
        v?.turn && (v.turn.toolCount !== counts.toolCount || v.turn.stepCount !== counts.stepCount)
          ? { ...v, turn: { ...v.turn, ...counts } }
          : v,
      );
    }
  } else {
    patchFeed(conv, (f) => {
      const rows = insertRow(f.rows, { k: 'entry', id: e.id, at: e.at });
      return rows === f.rows ? f : { ...f, rows };
    });
  }
}

export function applyEntryUpdate(conv: ConvId, e: Entry): void {
  if (!entryStore.get(key(conv, e.id))) {
    applyStep(conv, e, null);
    return;
  }
  putEntry(conv, e);
}

/** The live reply became its transcript line: the same place in the turn, a new id. */
export function applyEntryReplace(conv: ConvId, oldId: EntryId, e: Entry): void {
  commitAppends();
  const oldKey = key(conv, oldId);
  const had = entryStore.get(oldKey);
  putEntry(conv, e);
  entryStore.delete(oldKey);
  if (e.turnId) {
    const k = key(conv, e.turnId);
    const v = turnStore.get(k);
    if (v && v.entryIds.includes(oldId)) {
      const ids = v.entryIds.includes(e.id)
        ? v.entryIds.filter((x) => x !== oldId)
        : v.entryIds.map((x) => (x === oldId ? e.id : x));
      turnStore.set(k, { ...v, entryIds: ids });
    } else {
      addToTurn(conv, e.turnId, e.id, e.at);
    }
  } else {
    patchFeed(conv, (f) => {
      const replaced = f.rows.map((r) => (r.k === 'entry' && r.id === oldId ? { ...r, id: e.id } : r));
      return { ...f, rows: had ? replaced : insertRow(f.rows, { k: 'entry', id: e.id, at: e.at }) };
    });
  }
}

export function applyTurn(conv: ConvId, t: Turn): void {
  putTurn(conv, t);
  patchFeed(conv, (f) => {
    const rows = insertRow(f.rows, { k: 'turn', id: t.id, at: t.startedAt });
    return rows === f.rows ? f : { ...f, rows };
  });
  // «Сейчас» belongs to a live turn: no event clears it, the end of its turn does (wire decision 23).
  if (t.status !== 'live' && nowStore.get(conv)?.turnId === t.id) nowStore.delete(conv);
}

export function applyNow(conv: ConvId, now: Now): void {
  const have = nowStore.get(conv);
  if (have && have.at >= now.at) return;
  const t = turnStore.get(key(conv, now.turnId))?.turn;
  if (t && t.status !== 'live') return;
  nowStore.set(conv, now);
}

export function applyQueue(conv: ConvId, q: Queue): void {
  queueStore.patch(conv, (have) => (have && have.rev >= q.rev ? have : q));
}

export function applyQuestion(conv: ConvId, q: Question): void {
  gatesStore.patch(conv, (g = EMPTY_GATES) => {
    const questions = upsertById(g.questions, q);
    return questions === g.questions ? g : { ...g, questions };
  });
}

export function applyPermission(conv: ConvId, p: Permission): void {
  gatesStore.patch(conv, (g = EMPTY_GATES) => {
    const permissions = upsertById(g.permissions, p);
    return permissions === g.permissions ? g : { ...g, permissions };
  });
}

export function applyGateState(conv: ConvId, id: string, state: GateState): void {
  gatesStore.patch(conv, (g) => {
    if (!g) return g;
    if (id.startsWith('q:')) {
      if (!g.questions.some((q) => q.id === id && !same(q.state, state))) return g;
      return { ...g, questions: g.questions.map((q) => (q.id === id ? { ...q, state } : q)) };
    }
    if (!g.permissions.some((p) => p.id === id && !same(p.state, state))) return g;
    return { ...g, permissions: g.permissions.map((p) => (p.id === id ? { ...p, state } : p)) };
  });
}

export function applyBackground(conv: ConvId, task: BackgroundTask): void {
  backgroundStore.patch(conv, (list = []) => upsertById(list, task));
}

export function setShots(conv: ConvId, shots: Shot[]): void {
  shotsStore.patch(conv, (have) => (have && same(have, shots) ? have : shots));
}

export function setPictures(conv: ConvId, pictures: Picture[]): void {
  picturesStore.patch(conv, (have) => (have && same(have, pictures) ? have : pictures));
}

export function removeBackground(conv: ConvId, id: string): void {
  backgroundStore.patch(conv, (list) =>
    list?.some((x) => x.id === id) ? list.filter((x) => x.id !== id) : list,
  );
}

/** Merges turns and entries by id: entries keep their objects, a turn's entries keep their order by time. */
function mergeTurnsAndEntries(conv: ConvId, turns: Turn[], entries: Entry[], dropLive: boolean): FeedRow[] {
  commitAppends();
  const byTurn = new Map<TurnId, Entry[]>();
  const loose: FeedRow[] = [];
  for (const e of entries) {
    putEntry(conv, e);
    if (e.turnId) {
      const list = byTurn.get(e.turnId) ?? [];
      list.push(e);
      byTurn.set(e.turnId, list);
    } else {
      loose.push({ k: 'entry', id: e.id, at: e.at });
    }
  }
  const rows: FeedRow[] = [];
  const byId = new Map(turns.map((t) => [t.id, t]));
  for (const id of new Set<TurnId>([...byId.keys(), ...byTurn.keys()])) {
    const t = byId.get(id);
    if (t) putTurn(conv, t);
    const incoming = byTurn.get(id) ?? [];
    const k = key(conv, id);
    const v = turnStore.get(k) ?? { turn: null, entryIds: [] };
    const fresh = new Set(incoming.map((e) => e.id));
    const keep = dropLive ? v.entryIds.filter((x) => !isLive(x) || fresh.has(x)) : v.entryIds;
    const kept = new Set(keep);
    const added = incoming.filter((e) => !kept.has(e.id));
    if (added.length > 0 || keep.length !== v.entryIds.length || !turnStore.has(k)) {
      const at = (x: EntryId) => entryStore.get(key(conv, x))?.at ?? 0;
      // Both lists are in time order already: the new entries go between the kept ones by one linear merge.
      const order: EntryId[] = [];
      let j = 0;
      for (const x of keep) {
        const t = at(x);
        while (j < added.length && added[j].at < t) order.push(added[j++].id);
        order.push(x);
      }
      while (j < added.length) order.push(added[j++].id);
      turnStore.set(k, { ...v, entryIds: order });
      for (const x of v.entryIds) if (!kept.has(x)) entryStore.delete(key(conv, x));
    }
    rows.push({ k: 'turn', id, at: t?.startedAt ?? incoming[0]?.at ?? 0 });
  }
  return [...rows, ...loose];
}

/** A conv snapshot. replace: what the runtime has now (older pages stay); after: what follows the page's cursor. */
export function applyConvSnapshot(
  conv: ConvId,
  machine: MachineId,
  mode: 'replace' | 'after',
  data: ConvSnapshot,
  at: Ms,
): void {
  ensureFeed(conv, machine);
  const incoming = mergeTurnsAndEntries(conv, data.turns, data.entries, mode === 'replace');
  patchFeed(conv, (f) => {
    const dropLoose = (r: FeedRow) =>
      mode === 'replace' && r.k === 'entry' && isLive(r.id) && !incoming.some((x) => x.id === r.id);
    const rows = mergeRows(f.rows, incoming, dropLoose);
    return {
      ...f,
      loaded: true,
      source: 'live',
      rows,
      beforeTurnId: mode === 'replace' && !f.older ? data.beforeTurnId : f.beforeTurnId,
      subscribedAt: f.subscribedAt ?? at,
      error: null,
    };
  });
  if (data.now) nowStore.set(conv, data.now);
  else nowStore.delete(conv);
  queueStore.patch(conv, (have) => (have && have.rev > data.queue.rev ? have : data.queue));
  const gates = { questions: data.questions, permissions: data.permissions };
  gatesStore.patch(conv, (have) => (have && same(have, gates) ? have : gates));
  backgroundStore.patch(conv, (have) => (have && same(have, data.background) ? have : data.background));
  setShots(conv, data.shots ?? []);
  setPictures(conv, data.pictures ?? []);
}

/**
 * A conversation that conversation.create has just made has no history to wait for: its channel replays from the first
 * event, and the feed is complete (and empty) from the start. Without this the feed would stay unloaded, a skeleton,
 * because only a snapshot, the cache or an error say «loaded».
 */
export function startEmptyFeed(conv: ConvId, machine: MachineId, at: Ms): void {
  ensureFeed(conv, machine);
  patchFeed(conv, (f) =>
    f.loaded ? f : { ...f, loaded: true, source: 'live', subscribedAt: at, error: null },
  );
}

/** An older page of turns merged above the feed. */
export function applyOlderTurns(
  conv: ConvId,
  turns: Turn[],
  entries: Entry[],
  beforeTurnId: TurnId | null,
): void {
  const incoming = mergeTurnsAndEntries(conv, turns, entries, false);
  patchFeed(conv, (f) => ({
    ...f,
    rows: mergeRows(f.rows, incoming, () => false),
    beforeTurnId,
    older: true,
  }));
}

/** The steps of one turn, fetched when it unfolds. */
export function applyTurnEntries(conv: ConvId, turn: TurnId, entries: Entry[]): void {
  mergeTurnsAndEntries(
    conv,
    [],
    entries.map((e) => (e.turnId === turn ? e : { ...e, turnId: turn })),
    false,
  );
}

export function markFeedError(conv: ConvId, error: 'not-found'): void {
  patchFeed(conv, (f) => ({ ...f, loaded: true, error }));
}

/** Forgets the rows of a feed: its turns and their entries, the entries outside any turn. */
function dropRows(conv: ConvId, rows: FeedRow[]): void {
  for (const r of rows) {
    if (r.k === 'entry') entryStore.delete(key(conv, r.id));
    else {
      const v = turnStore.get(key(conv, r.id));
      for (const id of v?.entryIds ?? []) entryStore.delete(key(conv, id));
      turnStore.delete(key(conv, r.id));
    }
  }
}

/**
 * The feed as the runtime has it after a rewind or its undo (feed_reset): these turns are the feed, older pages too go.
 * A turn it held that is not among them goes with its steps, and so does one of the same id that started at another
 * time (Codex names a turn by its place in the transcript: the new branch's turn can have the old one's id). The turns
 * both branches share keep their steps, and the same reset again changes nothing.
 */
export function resetFeed(conv: ConvId, turns: Turn[], entries: Entry[], beforeTurnId: TurnId | null): void {
  commitAppends();
  const f = feedStore.get(conv);
  if (!f) return;
  const started = new Map(turns.map((t) => [t.id, t.startedAt]));
  const loose = new Set(entries.filter((e) => !e.turnId).map((e) => e.id));
  const goes = (r: FeedRow): boolean =>
    r.k === 'entry'
      ? !loose.has(r.id)
      : !started.has(r.id) || turnStore.get(key(conv, r.id))?.turn?.startedAt !== started.get(r.id);
  const gone = f.rows.filter(goes);
  const goneKeys = new Set(gone.map((r) => `${r.k} ${r.id}`));
  dropRows(conv, gone);
  const incoming = mergeTurnsAndEntries(conv, turns, entries, true);
  const rows = mergeRows(f.rows, incoming, (r) => goneKeys.has(`${r.k} ${r.id}`));
  patchFeed(conv, (v) =>
    v.rows === rows && v.beforeTurnId === beforeTurnId && !v.older && v.loaded && v.source === 'live'
      ? v
      : { ...v, loaded: true, source: 'live', rows, beforeTurnId, older: false },
  );
}

/** Forgets everything of a conversation (the cache let it go, or it was removed). */
export function dropConversationData(conv: ConvId): void {
  commitAppends();
  const f = feedStore.get(conv);
  if (f) dropRows(conv, f.rows);
  feedStore.delete(conv);
  nowStore.delete(conv);
  gatesStore.delete(conv);
  queueStore.delete(conv);
  backgroundStore.delete(conv);
  shotsStore.delete(conv);
  picturesStore.delete(conv);
  fedStore.delete(conv);
}

/** The last n turns of a feed with their entries: what IndexedDB keeps for the first frame after F5. */
export function exportFeed(conv: ConvId, turns: number, resume: Resume): CachedFeed | null {
  commitAppends();
  const f = feedStore.get(conv);
  if (!f || !f.loaded || f.source !== 'live' || f.error) return null;
  const turnRows = f.rows.filter((r) => r.k === 'turn');
  const from = turnRows.length > turns ? turnRows[turnRows.length - turns].at : -Infinity;
  const rows = f.rows.filter((r) => r.at >= from);
  const out: CachedFeed = {
    convId: conv,
    machineId: f.machineId,
    cursor: resume.cursor,
    lastEntryId: resume.lastEntryId,
    beforeTurnId:
      rows.length < f.rows.length
        ? (turnRows[turnRows.length - turns]?.id ?? f.beforeTurnId)
        : f.beforeTurnId,
    turns: [],
    entries: [],
  };
  out.state = {
    now: nowStore.get(conv) ?? null,
    queue: queueStore.get(conv) ?? null,
    questions: gatesStore.get(conv)?.questions ?? [],
    permissions: gatesStore.get(conv)?.permissions ?? [],
    background: backgroundStore.get(conv) ?? [],
  };
  for (const r of rows) {
    if (r.k === 'entry') {
      const e = entryStore.get(key(conv, r.id));
      if (e) out.entries.push(e);
      continue;
    }
    const v = turnStore.get(key(conv, r.id));
    if (!v?.turn) continue;
    out.turns.push(v.turn);
    for (const id of v.entryIds) {
      const e = entryStore.get(key(conv, id));
      if (e && !isLive(e.id)) out.entries.push(e);
    }
  }
  return out;
}

interface Resume {
  cursor: Cursor | null;
  lastEntryId: EntryId | null;
}

export interface CachedFeed {
  convId: ConvId;
  machineId: MachineId;
  cursor: Cursor | null;
  lastEntryId: EntryId | null;
  beforeTurnId: TurnId | null;
  turns: Turn[];
  entries: Entry[];
  /**
   * The parts of the conversation that only the snapshot carries, as they were at that cursor. The runtime answers a
   * page that comes back from its cursor with the events it missed, not a snapshot, so what lives only in a snapshot
   * (a gate's start, the queue, «Сейчас», a task's start) is lost on F5 unless the cache keeps it with the cursor. A
   * cache from before this field has no such state, so its cursor is not trusted (see cachedResume).
   */
  state?: CachedState;
}

/** The snapshot-only parts of a conversation (see CachedFeed.state). */
export interface CachedState {
  now: Now | null;
  queue: Queue | null;
  questions: Question[];
  permissions: Permission[];
  background: BackgroundTask[];
}

/**
 * Where a cached feed resumes from. A feed kept without its state resumes by its last entry only, so the runtime
 * answers with a snapshot after it, which carries the state: a cursor alone would replay events over nothing.
 */
export function cachedResume(c: CachedFeed): { cursor: Cursor | null; lastEntryId: EntryId | null } {
  return { cursor: c.state ? c.cursor : null, lastEntryId: c.lastEntryId };
}

/** Draws a cached feed before the network: source stays 'cached' until the channel's snapshot or events. */
export function importFeed(c: CachedFeed): void {
  if (feedStore.get(c.convId)?.loaded) return;
  ensureFeed(c.convId, c.machineId);
  const incoming = mergeTurnsAndEntries(c.convId, c.turns, c.entries, false);
  patchFeed(c.convId, (f) => ({
    ...f,
    loaded: true,
    source: 'cached',
    rows: mergeRows(f.rows, incoming, () => false),
    beforeTurnId: c.beforeTurnId,
  }));
  // What the channel has said since is replayed over it; a snapshot that came first has the last word.
  const st = c.state;
  if (!st) return;
  if (st.now && !nowStore.has(c.convId)) nowStore.set(c.convId, st.now);
  if (st.queue && !queueStore.has(c.convId)) queueStore.set(c.convId, st.queue);
  if (!gatesStore.has(c.convId))
    gatesStore.set(c.convId, { questions: st.questions ?? [], permissions: st.permissions ?? [] });
  if (!backgroundStore.has(c.convId)) backgroundStore.set(c.convId, st.background ?? []);
}
