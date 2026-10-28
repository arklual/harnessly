// Every dictation is kept in the browser while it is recorded (IndexedDB, a database of its own), so a dropped
// link, a release of the machine or F5 does not lose it: what could not be recognized stays as a failed take and is
// recognized again later, by hand or by itself once the machine is back. A recognized take is kept a day (the «I erased
// the text» case) and then forgotten. A take still being recorded in another tab writes `seen` every second; a page
// that finds one silent for longer counts it as interrupted. Every storage call is in try/catch: without IndexedDB
// dictation works the same, it only forgets on reload. Each person has a database of their own (hx-voice:<person>, as
// the drafts' hx-next:<person>) and the page holds only the signed-in person's takes: another account in the same
// browser never sees them. A take that is over (failed or recognized) goes to the cluster too (voice-cloud.ts), where
// it outlives the browser and the machine: the audio for 30 days, the text for good.
import { createKeyedStore, sameItems, useAll } from '@/next/core/store';
import { uuidv7 } from '@/next/core/ids';
import type { MachineId, Ms } from '@/next/protocol/v2';
import { meStore, personKey, useMe } from '@/next/stores/me';
import { deleteCloudTake, patchCloudTake, putCloudTake } from '@/next/features/composer/voice-cloud';

export type TakeState = 'live' | 'failed' | 'done';

export interface VoiceTake {
  id: string;
  /** Whose it is: personKey of the account that recorded it; '' while that was not known (kept in the page only). */
  person: string;
  machine: MachineId;
  at: Ms;
  /** The last time the recording page wrote it. */
  seen: Ms;
  mime: string;
  /** How long it is, by the recording's clock. */
  ms: number;
  state: TakeState;
  /** What was recognized; null until then. */
  text: string | null;
  audio: Blob;
  /** The cluster has its audio (and its state and text as of the last write there). */
  cloud?: boolean;
}

const DB = 'hx-voice';
const STORE = 'takes';
/** A recognized take is kept this long. */
const KEEP_DONE_MS = 24 * 60 * 60 * 1000;
/** A live take nobody wrote for this long belongs to a page that is gone. */
const SILENT_MS = 10_000;

export const takesStore = createKeyedStore<string, VoiceTake>();

const personNow = (): string | null => {
  const me = meStore.get();
  return me.k === 'ready' ? personKey(me.me) : null;
};

let base: { person: string; db: Promise<IDBDatabase | null> } | null = null;

/** The person's database; none for a take whose person is not known. */
const open = (person: string): Promise<IDBDatabase | null> => {
  if (!person) return Promise.resolve(null);
  if (base?.person === person) return base.db;
  const db = new Promise<IDBDatabase | null>((resolve) => {
    try {
      const req = indexedDB.open(`${DB}:${person}`, 1);
      req.onupgradeneeded = () => req.result.createObjectStore(STORE, { keyPath: 'id' });
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => resolve(null);
      req.onblocked = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
  base = { person, db };
  return db;
};

const run = async <T>(
  person: string,
  mode: IDBTransactionMode,
  fn: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T | undefined> => {
  const db = await open(person);
  if (!db) return undefined;
  return new Promise((resolve) => {
    try {
      const req = fn(db.transaction(STORE, mode).objectStore(STORE));
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => resolve(undefined);
    } catch {
      resolve(undefined);
    }
  });
};

const write = (take: VoiceTake) => {
  takesStore.set(take.id, take);
  void run(take.person, 'readwrite', (store) => store.put(take));
};

const remove = (take: VoiceTake) => {
  takesStore.delete(take.id);
  void run(take.person, 'readwrite', (store) => store.delete(take.id));
};

/** Takes on their way to the cluster: one write at a time each. */
const syncing = new Set<string>();

/** A take that is over goes to the cluster once (its audio), and again with its text when that comes. */
const sync = async (id: string) => {
  const take = takesStore.get(id);
  if (!take || !take.person || take.state === 'live' || syncing.has(id)) return;
  syncing.add(id);
  try {
    if (!take.cloud) {
      if (take.audio.size === 0) return;
      const state = take.state;
      if (!(await putCloudTake({ ...take, state }))) return;
      const now = takesStore.get(id);
      if (!now) {
        // Dropped while it was uploading: it goes from the cluster too.
        void deleteCloudTake(id);
        return;
      }
      write({ ...now, cloud: true });
      if (now.state === 'done' && now.text) await patchCloudTake(id, { state: 'done', text: now.text });
      return;
    }
    await patchCloudTake(id, { state: take.state, text: take.text });
  } finally {
    syncing.delete(id);
  }
};

let loaded: { person: string; done: Promise<void> } | null = null;

/** Reads what the person's earlier pages left: interrupted recordings become failed takes, old recognized ones go. */
export const loadTakes = (): Promise<void> => {
  const person = personNow();
  if (!person) return Promise.resolve();
  if (loaded?.person === person) return loaded.done;
  const done = (async () => {
    const all = (await run<VoiceTake[]>(person, 'readonly', (store) => store.getAll())) ?? [];
    if (personNow() !== person) return;
    const now = Date.now();
    for (const take of all) {
      if (takesStore.has(take.id)) continue;
      if (take.state === 'done' && now - take.at > KEEP_DONE_MS) remove(take);
      else if (take.state === 'live' && now - take.seen > SILENT_MS) write({ ...take, state: 'failed' });
      else takesStore.set(take.id, take);
    }
    // What an earlier page could not hand over to the cluster goes now.
    for (const take of takesStore.values()) if (!take.cloud && take.state !== 'live') void sync(take.id);
  })();
  loaded = { person, done };
  return done;
};

// Another account signed in, or nobody: the takes of the one before leave the page (they stay in their database).
meStore.subscribe(() => {
  const person = personNow();
  for (const take of [...takesStore.values()]) {
    if (take.person && take.person !== person) takesStore.delete(take.id);
  }
  if (person) void loadTakes();
});

export const startTake = (machine: MachineId, mime: string): VoiceTake => {
  const now = Date.now();
  const take: VoiceTake = {
    id: uuidv7(now),
    person: personNow() ?? '',
    machine,
    at: now,
    seen: now,
    mime,
    ms: 0,
    state: 'live',
    text: null,
    audio: new Blob([], { type: mime }),
  };
  write(take);
  return take;
};

/** The recording so far; written at most once a second while it goes on. */
export const recordTake = (id: string, chunks: Blob[]) => {
  const take = takesStore.get(id);
  if (!take) return;
  const now = Date.now();
  write({ ...take, seen: now, ms: now - take.at, audio: new Blob(chunks, { type: take.mime }) });
};

export const failTake = (id: string) => {
  const take = takesStore.get(id);
  if (!take || take.state === 'done') return;
  write({ ...take, state: 'failed' });
  void sync(id);
};

export const finishTake = (id: string, text: string) => {
  const take = takesStore.get(id);
  if (!take) return;
  write({ ...take, state: 'done', text });
  void sync(id);
};

export const dropTake = (id: string) => {
  const take = takesStore.get(id);
  if (!take) return;
  remove(take);
  if (take.cloud) void deleteCloudTake(id);
};

/** The signed-in person's takes of the machine that wait to be recognized, oldest first. */
export const useFailedTakes = (machine: MachineId | null): VoiceTake[] => {
  const me = useMe();
  const person = me.k === 'ready' ? personKey(me.me) : null;
  return useAll(
    takesStore,
    (s) =>
      [...s.values()]
        .filter(
          (take) =>
            (take.person === person || take.person === '') &&
            take.machine === machine &&
            take.state === 'failed',
        )
        .sort((a, b) => a.at - b.at),
    sameItems,
  );
};
