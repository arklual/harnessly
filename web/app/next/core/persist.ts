// What survives F5 (spec §6): composer drafts, outgoing messages the runtime has not shown yet, and the last six
// chats (three turns each) with the sidebar's list of the active machine, so the first frame after a reload draws
// before the network, the list together with the feed. IndexedDB, every call in
// try/catch: in a private window or with storage blocked the page works the same, it only forgets on reload.

import { pageClock } from '@/next/core/clock';
import { setResume } from '@/next/core/realtime/cursors';
import { batch } from '@/next/core/store';
import {
  onRecentChange,
  recentConversations,
  STORED_CHATS,
  STORED_TURNS,
  touchConversation,
} from '@/next/stores/cache';
import {
  backgroundStore,
  cachedResume,
  entryStore,
  gatesStore,
  queueStore,
  exportFeed,
  feedStore,
  importFeed,
  turnStore,
  type CachedFeed,
} from '@/next/stores/conversation';
import {
  conversationsStore,
  convsCachedStore,
  convsLoadedStore,
  byActivity,
} from '@/next/stores/conversations';
import { draftsStore, emptyDraft, type Draft, type DraftKey } from '@/next/stores/drafts';
import { stashStore, type StashItem } from '@/next/stores/stash';
import { draftBaseStore, type DraftBase } from '@/next/stores/draft-sync';
import { activeMachineStore } from '@/next/stores/machines';
import { defaultCwdStore, projectsStore } from '@/next/stores/projects';
import {
  convMessagesStore,
  isTerminal,
  messagesStore,
  putLocal,
  type MessageView,
} from '@/next/stores/messages';
import { resumeOf } from '@/next/core/realtime/cursors';
import type { Conversation, ConvId, MachineId, Project } from '@/next/protocol/v2';

export type Area = 'drafts' | 'outbox' | 'feeds' | 'convs' | 'stash';
const AREAS: Area[] = ['drafts', 'outbox', 'feeds', 'convs', 'stash'];

/** A key-value store per area; never throws, a failure reads as nothing. */
export interface KV {
  get<T>(area: Area, key: string): Promise<T | undefined>;
  set(area: Area, key: string, value: unknown): Promise<void>;
  del(area: Area, key: string): Promise<void>;
  all<T>(area: Area): Promise<[string, T][]>;
}

export function memoryKV(): KV & { dump(): Record<Area, Map<string, unknown>> } {
  const data: Record<Area, Map<string, unknown>> = {
    drafts: new Map(),
    outbox: new Map(),
    feeds: new Map(),
    convs: new Map(),
    stash: new Map(),
  };
  const clone = <T>(v: T): T => (v === undefined ? v : (JSON.parse(JSON.stringify(v)) as T));
  return {
    get: async (a, k) => clone(data[a].get(k)) as never,
    set: async (a, k, v) => void data[a].set(k, clone(v)),
    del: async (a, k) => void data[a].delete(k),
    all: async (a) => [...data[a].entries()].map(([k, v]) => [k, clone(v)]) as never,
    dump: () => data,
  };
}

/** The person whose browser data this is when the runtime does not name one (standalone). */
export const LOCAL_PERSON = 'local';

/** One IndexedDB per person: a drafts, outbox or cached chat of one account is never read as another's. */
export const dbNameOf = (person: string): string => `hx-next:${person}`;

export function idbKV(name = dbNameOf(LOCAL_PERSON)): KV {
  let db: Promise<IDBDatabase | null> | null = null;
  const open = () =>
    (db ??= new Promise<IDBDatabase | null>((resolve) => {
      try {
        const req = indexedDB.open(name, 3);
        req.onupgradeneeded = () => {
          for (const a of AREAS)
            if (!req.result.objectStoreNames.contains(a)) req.result.createObjectStore(a);
        };
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => resolve(null);
        req.onblocked = () => resolve(null);
      } catch {
        resolve(null);
      }
    }));
  const run = async <T>(area: Area, mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>) => {
    const d = await open();
    if (!d) return undefined;
    return new Promise<T | undefined>((resolve) => {
      try {
        const req = fn(d.transaction(area, mode).objectStore(area));
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => resolve(undefined);
      } catch {
        resolve(undefined);
      }
    });
  };
  return {
    get: (a, k) => run(a, 'readonly', (s) => s.get(k)),
    set: async (a, k, v) => void (await run(a, 'readwrite', (s) => s.put(v, k))),
    del: async (a, k) => void (await run(a, 'readwrite', (s) => s.delete(k))),
    async all<T>(a: Area) {
      const keys = ((await run(a, 'readonly', (s) => s.getAllKeys())) ?? []) as IDBValidKey[];
      const values = ((await run(a, 'readonly', (s) => s.getAll())) ?? []) as T[];
      return keys.length === values.length ? keys.map((k, i) => [String(k), values[i]] as [string, T]) : [];
    },
  };
}

export const DRAFT_DELAY_MS = 300;
export const FEEDS_DELAY_MS = 1000;
export const CONVS_DELAY_MS = 1000;
/** The sidebar's list is kept for this many chats, the most recent ones. */
export const STORED_LIST = 300;

/** The sidebar's list of the active machine as its snapshot last showed it. */
export interface CachedList {
  machineId: MachineId;
  conversations: Conversation[];
  projects: Project[];
  defaultCwd: string | null;
}
const LIST_KEY = 'list';

/** A draft as IndexedDB keeps it: the field and, once the machine has it, the revision its text came from. */
type KeptDraft = Draft & { base?: Omit<DraftBase, 'attachments'> & Partial<Pick<DraftBase, 'attachments'>> };

/** A cached chat with the messages the runtime has shown for it: their states live in the snapshot only, like the gates. */
type KeptFeed = CachedFeed & { rank: number; messages?: MessageView[] };

/** The runtime's messages of a chat that are not over: the queue and the steer in its input stand on them after F5. */
const keptMessages = (conv: ConvId): MessageView[] =>
  (convMessagesStore.get(conv) ?? [])
    .map((id) => messagesStore.get(id))
    .filter((m): m is MessageView => !!m && m.rev > 0 && !isTerminal(m.state));

/** What the outbox keeps: a message the runtime has not shown yet and that is still meant to go. */
export const keptInOutbox = (m: MessageView): boolean =>
  m.rev === 0 && (m.state.k === 'submitting' || m.state.k === 'failed' || m.state.k === 'uploading');

export interface PersistOptions {
  /** Whose data this is; an outbox record written for someone else is not taken back. */
  person?: string;
  /** false: drafts and chats only; the outbox waits for enableOutbox() (after the person is known). */
  outbox?: boolean;
}

/** An outbox record with the person it was written for. */
type KeptMessage = MessageView & { person?: string };

export interface Persistence {
  person: string;
  /** Resolves when what was kept is back in the stores. */
  ready: Promise<void>;
  /** Takes the outbox back into the stores and starts keeping it; once the person is confirmed. */
  enableOutbox(): Promise<void>;
  /** Writes everything pending now (tests, pagehide). */
  flushNow(): Promise<void>;
  stop(): void;
}

export function startPersistence(kv: KV, o: PersistOptions = {}): Persistence {
  const person = o.person ?? LOCAL_PERSON;
  const clock = pageClock();
  const draftTimers = new Map<DraftKey, unknown>();
  /** The fields changed before what IndexedDB kept was back: what it kept of them is older. */
  const touchedDrafts = new Set<DraftKey>();
  let draftsLoaded = false;
  let feedsTimer: unknown = null;
  let listTimer: unknown = null;
  const pending = new Set<Promise<void>>();
  const track = (p: Promise<void>) => {
    pending.add(p);
    void p.finally(() => pending.delete(p));
  };

  let unOutbox: (() => void) | null = null;
  let stopped = false;
  let outboxLoaded: Promise<void> | null = null;

  const loadOutbox = async () => {
    const kept = await kv.all<KeptMessage>('outbox');
    batch(() => {
      for (const [, { person: owner, ...m }] of kept)
        // A record kept before the page noted when its state began counts from its making.
        if (owner === person && !messagesStore.has(m.clientId))
          putLocal({ ...m, stateAt: m.stateAt ?? m.createdAt });
    });
    if (stopped) return;
    unOutbox = messagesStore.subscribeAll((changed) => {
      for (const id of changed) {
        const m = messagesStore.get(id);
        track(m && keptInOutbox(m) ? kv.set('outbox', id, { ...m, person }) : kv.del('outbox', id));
      }
    });
  };

  const enableOutbox = () => (outboxLoaded ??= loadOutbox().catch(() => {}));

  const ready = (async () => {
    const [drafts, feeds, list, stashed] = await Promise.all([
      kv.all<KeptDraft>('drafts'),
      kv.all<KeptFeed>('feeds'),
      kv.get<CachedList>('convs', LIST_KEY),
      kv.all<StashItem[]>('stash'),
    ]);
    if (o.outbox !== false) await enableOutbox();
    draftsLoaded = true;
    batch(() => {
      for (const [k, { base, ...d }] of drafts) {
        const key = k as DraftKey;
        const now = draftBaseStore.get(key);
        // What was kept gives way to what happened since the page opened: a field changed or sent meanwhile (the read
        // of IndexedDB began before), or a draft the machine has moved past (its snapshot came first). Otherwise a
        // message sent in the first moments would come back into the field, files and all.
        const stale =
          touchedDrafts.has(key) || (base !== undefined && now !== undefined && base.rev < now.rev);
        if (!stale && !draftsStore.has(key) && !emptyDraft(d)) draftsStore.set(key, d);
        // A base kept before the files went with the text has none of them.
        if (base && (!now || now.rev < base.rev))
          draftBaseStore.set(key, { ...base, attachments: base.attachments ?? [] });
      }
      // The least recent first, so the most recent ends at the head of the cache as it was before the reload.
      for (const [, f] of feeds.sort((a, b) => b[1].rank - a[1].rank)) {
        // A chat whose snapshot came first has the runtime's word on its messages: the cached ones would bring back
        // what the runtime has finished since, so they are taken only before it.
        const before = !feedStore.get(f.convId)?.loaded;
        importFeed(f);
        if (before) for (const m of f.messages ?? []) if (!messagesStore.has(m.clientId)) putLocal(m);
        setResume(f.machineId, `conv:${f.convId}`, cachedResume(f));
        touchConversation(f.convId);
      }
      if (list) importList(list);
      // A draft put aside before the page opened; one put aside since is the newer word.
      for (const [k, items] of stashed)
        if (!stashStore.has(k as DraftKey) && Array.isArray(items) && items.length > 0)
          stashStore.set(k as DraftKey, items);
    });
  })().catch(() => {});

  const writeDraft = (k: DraftKey) => {
    draftTimers.delete(k);
    const d = draftsStore.get(k);
    const base = draftBaseStore.get(k);
    // An empty field is kept while it has a base: the revision of a clear is what keeps an older text from coming back.
    let kept: KeptDraft | null = null;
    if (d) kept = base ? { ...d, base } : d;
    else if (base) kept = { text: '', attachments: [], base };
    track(kept ? kv.set('drafts', k, kept) : kv.del('drafts', k));
  };

  const writeFeeds = async () => {
    feedsTimer = null;
    const keep = recentConversations(STORED_CHATS);
    const have = await kv.all<CachedFeed>('feeds');
    for (const [k] of have) if (!keep.includes(k as ConvId)) await kv.del('feeds', k);
    for (const [rank, conv] of keep.entries()) {
      const machine = feedStore.get(conv)?.machineId;
      const f = machine ? exportFeed(conv, STORED_TURNS, resumeOf(machine, `conv:${conv}`)) : null;
      if (f) await kv.set('feeds', conv, { ...f, messages: keptMessages(conv), rank });
    }
  };

  const writeList = () => {
    listTimer = null;
    const list = exportList();
    track(list ? kv.set('convs', LIST_KEY, list) : Promise.resolve());
  };
  const scheduleList = () => {
    if (listTimer !== null) return;
    listTimer = clock.setTimeout(writeList, CONVS_DELAY_MS);
  };
  const unList = [
    conversationsStore.subscribeAll(scheduleList),
    projectsStore.subscribeAll(scheduleList),
    convsLoadedStore.subscribeAll(scheduleList),
  ];

  const writeDraftLater = (k: DraftKey) => {
    clock.clearTimeout(draftTimers.get(k));
    draftTimers.set(
      k,
      clock.setTimeout(() => writeDraft(k), DRAFT_DELAY_MS),
    );
  };
  const unDraftText = draftsStore.subscribeAll((changed) => {
    for (const k of changed) {
      if (!draftsLoaded) touchedDrafts.add(k);
      writeDraftLater(k);
    }
  });
  const unStash = stashStore.subscribeAll((changed) => {
    for (const k of changed) {
      const items = stashStore.get(k);
      track(items ? kv.set('stash', k, items) : kv.del('stash', k));
    }
  });
  const unDraftBase = draftBaseStore.subscribeAll((changed) => changed.forEach(writeDraftLater));
  const unDrafts = () => {
    unDraftText();
    unDraftBase();
  };
  const scheduleFeeds = () => {
    if (feedsTimer !== null) return;
    feedsTimer = clock.setTimeout(() => track(writeFeeds()), FEEDS_DELAY_MS);
  };
  const unRecent = onRecentChange(scheduleFeeds);
  // A feed changes far more often than it needs saving: at most once a second, the six recent ones.
  const unFeeds = feedStoreChanges((conv) => {
    if (recentConversations(STORED_CHATS).includes(conv)) scheduleFeeds();
  });

  return {
    person,
    ready,
    enableOutbox,
    async flushNow() {
      for (const [k, t] of draftTimers) {
        clock.clearTimeout(t);
        writeDraft(k);
      }
      if (feedsTimer !== null) {
        clock.clearTimeout(feedsTimer);
        track(writeFeeds());
      }
      if (listTimer !== null) {
        clock.clearTimeout(listTimer);
        writeList();
      }
      while (pending.size > 0) await Promise.all([...pending]);
    },
    stop() {
      stopped = true;
      unDrafts();
      unStash();
      unOutbox?.();
      unRecent();
      unFeeds();
      unList.forEach((off) => off());
      for (const t of draftTimers.values()) clock.clearTimeout(t);
      if (feedsTimer !== null) clock.clearTimeout(feedsTimer);
      if (listTimer !== null) clock.clearTimeout(listTimer);
    },
  };
}

/**
 * The list to keep: the active machine's, once that machine itself has answered with it (a list that was only the
 * cache is not written back as if it were fresh).
 */
function exportList(): CachedList | null {
  const machine = activeMachineStore.get();
  if (!machine || !convsLoadedStore.has(machine)) return null;
  const conversations = [...conversationsStore.values()]
    .filter((c) => c.machineId === machine)
    .sort(byActivity)
    .slice(0, STORED_LIST);
  const projects = [...projectsStore.values()].filter((p) => p.machineId === machine);
  return { machineId: machine, conversations, projects, defaultCwd: defaultCwdStore.get(machine) ?? null };
}

/** Puts the kept list where the machine's snapshot will, marked as the cache's until that snapshot replaces it. */
function importList(list: CachedList): void {
  const { machineId } = list;
  if (convsLoadedStore.has(machineId) || !Array.isArray(list.conversations)) return;
  for (const c of list.conversations) if (!conversationsStore.has(c.id)) conversationsStore.set(c.id, c);
  for (const p of list.projects) if (!projectsStore.has(p.id)) projectsStore.set(p.id, p);
  if (list.defaultCwd !== null && !defaultCwdStore.has(machineId))
    defaultCwdStore.set(machineId, list.defaultCwd);
  convsCachedStore.set(machineId, true);
}

/** Hears which conversations' feeds changed (rows, turns, entries or background tasks). */
function feedStoreChanges(fn: (conv: ConvId) => void): () => void {
  const of = (k: string) => k.slice(0, k.indexOf(' ')) as ConvId;
  const a = feedStore.subscribeAll((ks) => ks.forEach((k) => fn(k)));
  const d = backgroundStore.subscribeAll((ks) => ks.forEach((k) => fn(k)));
  // The queue, the gates and the messages are kept with the cursor (`now` is not: it ticks, and a replay brings it).
  const e = queueStore.subscribeAll((ks) => ks.forEach((k) => fn(k)));
  const g = gatesStore.subscribeAll((ks) => ks.forEach((k) => fn(k)));
  const m = messagesStore.subscribeAll((ks) => {
    for (const id of ks) {
      const conv = messagesStore.get(id)?.conversationId;
      if (conv) fn(conv);
    }
  });
  const b = turnStore.subscribeAll((ks) => new Set([...ks].map(of)).forEach(fn));
  const c = entryStore.subscribeAll((ks) => new Set([...ks].map(of)).forEach(fn));
  return () => {
    a();
    b();
    c();
    d();
    e();
    g();
    m();
  };
}
