// The v2 bootstrap of the new interface: what IndexedDB kept for the person seen last, me, then the
// outbox of the confirmed person, machines and one socket per machine. A different person on this page is a clean
// start: nothing of the previous one stays in memory, and an outbox is never sent under another's name.

import { useSyncExternalStore } from 'react';
import { pageClock } from '@/next/core/clock';
import { getPage } from '@/next/core/net/http';
import { clusterApi, machineApi } from '@/next/core/net/machine';
import { startDraftSync } from '@/next/core/drafts-cloud';
import { dbNameOf, idbKV, startPersistence, type KV, type Persistence } from '@/next/core/persist';
import { hub, type HubOptions } from '@/next/core/realtime/hub';
import { clearPersonData } from '@/next/core/session';
import { batch, createStore } from '@/next/core/store';
import { startOutbox } from '@/next/commands/messages';
import { httpMeSource, loadMe, meStore, personKey, setMeSource, type MeSource } from '@/next/stores/me';
import { machinesStore, putMachines } from '@/next/stores/machines';
import { parsePage, type Machine, type MachineId } from '@/next/protocol/v2';

/** IndexedDB gets this long to give back the cached chats before the network starts. */
export const CACHE_WAIT_MS = 300;

export type BootState =
  | { k: 'starting' }
  | { k: 'signed-out' }
  | { k: 'ready'; machines: MachineId[] }
  | { k: 'failed'; code: 'timeout' | 'network' | 'http' };

export const bootStore = createStore<BootState>({ k: 'starting' });

/** Standalone has one machine, the runtime that served the page. */
const LOCAL: Machine = {
  id: 'local',
  name: '',
  host: typeof location === 'undefined' ? '' : location.host,
  reach: 'online',
  lastSeenAt: null,
  runtime: null,
  agents: [],
};

async function loadMachines(): Promise<Machine[] | 'signed-out' | { code: 'timeout' | 'network' | 'http' }> {
  const res = await getPage(clusterApi('/machines'), (v) => v as Machine[], 10000);
  if ('ok' in res) return Array.isArray(res.value) ? res.value : [LOCAL];
  // Standalone, or a gateway before stage E: no fleet list, the runtime that served the page is the one machine.
  if (res.r === 'refused') return res.code === 'not-found' ? [LOCAL] : { code: 'http' };
  if (res.code !== 'http') return { code: res.code };
  if (res.status === 401 || res.status === 403) return 'signed-out';
  return res.status === 404 ? [LOCAL] : { code: 'http' };
}

/** GET /api/v2/hello: a machine without v2 is listed as an older runtime, its chats are not (spec §8). Only an answer
 *  says so — an older protocol, no such page, a page that is not v2's; a machine that did not answer (timeout, network,
 *  a gateway's 5xx) is not «outdated», its socket keeps trying and the pill says «Нет связи». */
async function checkRuntime(m: MachineId): Promise<boolean> {
  const res = await getPage(machineApi(m).http('/hello'), parsePage.hello, 10000);
  const outdated = isOutdated(res);
  machinesStore.patch(m, (v) =>
    v ? { ...v, runtime: outdated ? 'outdated' : 'ok' in res ? 'v2' : 'unknown' } : v,
  );
  return !outdated;
}

const isOutdated = (res: Awaited<ReturnType<typeof getPage<{ proto: number }>>>): boolean => {
  if ('ok' in res) return res.value.proto < 2;
  if (res.r === 'refused') return res.code === 'not-found';
  return res.code === 'http' && res.status < 500 && res.status !== 401 && res.status !== 403;
};

export interface BootOptions {
  /** The person's IndexedDB; by default `hx-next:<person>`. */
  kv?: (person: string) => KV;
  me?: MeSource;
  hub?: HubOptions;
  /** Whose data to draw before me answers; by default what this browser kept last. */
  lastPerson?: string | null;
}

/** Who this browser last served: its chats and drafts may draw before the network says who is signed in now. */
const HINT_KEY = 'hx-next-person';

const readHint = (): string | null => {
  try {
    return localStorage.getItem(HINT_KEY);
  } catch {
    return null;
  }
};

const writeHint = (person: string): void => {
  try {
    localStorage.setItem(HINT_KEY, person);
  } catch {
    // Storage blocked: the next load starts without a cache, which is only slower.
  }
};

let booted: Promise<BootState> | null = null;
/** Bumped by shutdownNext: a boot that was let go finishes without touching the next person's stores. */
let generation = 0;
let persistence: Persistence | null = null;
let stopOutbox: (() => void) | null = null;
let stopDraftSync: (() => void) | null = null;

const flushOnHide = () => void persistence?.flushNow();
// A phone puts a page in the background and may end it there without a pagehide: what was typed is written as the
// page goes out of sight.
const flushOnHidden = () => {
  if (document.visibilityState === 'hidden') flushOnHide();
};

const withinCacheWait = (ready: Promise<void>) =>
  Promise.race([ready, new Promise<void>((r) => pageClock().setTimeout(r, CACHE_WAIT_MS))]);

/** Starts v2 once per page: cache of the last person → me → (their outbox) → machines → sockets. */
export function bootNext(o: BootOptions = {}): Promise<BootState> {
  const mine = generation;
  const gone = () => mine !== generation;
  booted ??= (async () => {
    if (typeof window !== 'undefined') {
      window.addEventListener('pagehide', flushOnHide);
      document.addEventListener('visibilitychange', flushOnHidden);
    }
    const openKV = o.kv ?? ((person: string) => idbKV(dbNameOf(person)));
    const hint = o.lastPerson === undefined ? readHint() : o.lastPerson;
    // Drafts and chats of the last person draw at once; their outbox waits until me says it is still them.
    if (hint) {
      persistence = startPersistence(openKV(hint), { person: hint, outbox: false });
      await withinCacheWait(persistence.ready);
    }
    setMeSource(o.me ?? httpMeSource());
    if (gone()) return bootStore.get();
    const me = await loadMe();
    if (gone()) return bootStore.get();
    if (me.k === 'signed-out') {
      persistence?.stop();
      persistence = null;
      clearPersonData();
      meStore.set(me);
      return set({ k: 'signed-out' });
    }
    if (me.k === 'ready') {
      const person = personKey(me.me);
      if (persistence?.person === person) {
        await persistence.enableOutbox();
      } else {
        // Another person than the one this browser served last: nothing of the previous one is kept in memory.
        persistence?.stop();
        clearPersonData();
        meStore.set(me);
        persistence = startPersistence(openKV(person), { person });
        await withinCacheWait(persistence.ready);
      }
      writeHint(person);
      stopOutbox = startOutbox();
      stopDraftSync = startDraftSync();
    }
    const machines = await loadMachines();
    if (gone()) return bootStore.get();
    if (machines === 'signed-out') return set({ k: 'signed-out' });
    if (!Array.isArray(machines)) return set({ k: 'failed', code: machines.code });
    batch(() => putMachines(machines));
    const v2 = (
      await Promise.all(machines.map(async (m) => ((await checkRuntime(m.id)) ? m.id : null)))
    ).filter((id): id is MachineId => id !== null);
    if (gone()) return bootStore.get();
    if (o.hub) hub.configure(o.hub);
    const preferred = me.k === 'ready' ? me.me.settings.defaultMachine : null;
    hub.start(v2, preferred && v2.includes(preferred) ? preferred : (v2[0] ?? null));
    return set({ k: 'ready', machines: v2 });
  })();
  return booted;
}

/** The page lets this person go (sign-out, another account): sockets, outbox, cache and stores. */
export async function shutdownNext(): Promise<void> {
  const p = persistence;
  persistence = null;
  stopOutbox?.();
  stopOutbox = null;
  stopDraftSync?.();
  stopDraftSync = null;
  if (typeof window !== 'undefined') {
    window.removeEventListener('pagehide', flushOnHide);
    document.removeEventListener('visibilitychange', flushOnHidden);
  }
  booted = null;
  generation += 1;
  // What was typed in the last second still reaches the person's own database.
  await p?.flushNow();
  p?.stop();
  clearPersonData();
  bootStore.set({ k: 'starting' });
}

const set = (s: BootState): BootState => {
  bootStore.set(s);
  return s;
};

export const useBoot = (): BootState =>
  useSyncExternalStore(bootStore.subscribe, bootStore.get, bootStore.get);
