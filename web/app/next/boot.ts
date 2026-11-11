// The v2 bootstrap of the new interface: what IndexedDB kept for the person seen last, me, then the
// outbox of the confirmed person, machines and one socket per machine. A different person on this page is a clean
// start: nothing of the previous one stays in memory, and an outbox is never sent under another's name.

import { useSyncExternalStore } from 'react';
import { pageClock } from '@/next/core/clock';
import { getPage } from '@/next/core/net/http';
import { clusterApi, machineApi } from '@/next/core/net/machine';
import { startDraftSync } from '@/next/core/drafts-cloud';
import { watchFleet, type FleetSource, type FleetWatch } from '@/next/core/fleet-watch';
import { startJournal } from '@/next/core/journal';
import { dbNameOf, idbKV, startPersistence, type KV, type Persistence } from '@/next/core/persist';
import { hub, type HubOptions } from '@/next/core/realtime/hub';
import { clearPersonData } from '@/next/core/session';
import { batch, createStore } from '@/next/core/store';
import { startOutbox } from '@/next/commands/messages';
import {
  httpMeSource,
  loadMe,
  meStore,
  personKey,
  setMeSource,
  type MeSource,
  type MeState,
} from '@/next/stores/me';
import { forgetCentralList, refreshCentral } from '@/next/stores/central';
import { connectionStore } from '@/next/stores/connection';
import { machinesStore, mergeMachines, putMachines, type FleetMachine } from '@/next/stores/machines';
import { seedLimits } from '@/next/bindings/use-limits';
import { queuedChatsStore, watchQueuedChats } from '@/next/commands/queued-chats';
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

/** How long a runtime that is still starting (503) is let be before /machines is asked again. */
const STARTING_RETRY_MS = 1000;

async function loadMachines(
  gone: () => boolean = () => false,
): Promise<FleetMachine[] | 'signed-out' | { code: 'timeout' | 'network' | 'http' }> {
  let res = await getPage(clusterApi('/machines'), (v) => v as FleetMachine[], 10000);
  // A standalone runtime answers 503 ({r:'refused',code:'timeout'}) while its core starts: the kept list is drawn
  // meanwhile, and the machines are asked again until it answers.
  const starting = (r: typeof res) =>
    !('ok' in r) && (r.r === 'refused' ? r.code === 'timeout' : r.code === 'http' && r.status === 503);
  while (starting(res) && !gone()) {
    await new Promise((resolve) => setTimeout(resolve, STARTING_RETRY_MS));
    res = await getPage(clusterApi('/machines'), (v) => v as FleetMachine[], 10000);
  }
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
  // A /hello that did not answer says nothing new: the socket, which opens beside it, may have told already.
  machinesStore.patch(m, (v) => {
    if (!v) return v;
    if (outdated) return { ...v, runtime: 'outdated' };
    if (!('ok' in res)) return v;
    // The runtime answered: whatever the hub said of it hanging is over.
    const { hangs: _hangs, ...answered } = v;
    return { ...answered, runtime: 'v2' };
  });
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
  /** Opens the hub's fleet stream; by default an EventSource. */
  fleetSource?: (url: string) => FleetSource | null;
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
let stopQueuedChats: (() => void) | null = null;
let stopJournal: (() => void) | null = null;
let fleetWatch: FleetWatch | null = null;
let stopFleetNudge: (() => void) | null = null;

const flushOnHide = () => void persistence?.flushNow();
// A phone puts a page in the background and may end it there without a pagehide: what was typed is written as the
// page goes out of sight.
const flushOnHidden = () => {
  if (document.visibilityState === 'hidden') flushOnHide();
};

/**
 * How long the first screen waits for me before it draws the machines. me is the home machine's answer; a
 * home machine whose agent keeps its link while its runtime hangs answered it late or never, and a fresh device, with
 * nothing kept to draw, stayed blank for that long. The list and the chats are the hub's; me comes when it comes.
 */
export const ME_FIRST_SCREEN_MS = 3000;

const withinCacheWait = (ready: Promise<void>) =>
  Promise.race([ready, new Promise<void>((r) => pageClock().setTimeout(r, CACHE_WAIT_MS))]);

const signOut = (me: MeState): BootState => {
  persistence?.stop();
  persistence = null;
  clearPersonData();
  // The list was this person's: whoever signs in next is not shown it.
  forgetCentralList();
  meStore.set(me);
  return set({ k: 'signed-out' });
};

/** The person me confirmed: their cache and outbox (a different person than the hint is a clean start). */
async function adopt(me: Extract<MeState, { k: 'ready' }>, openKV: (person: string) => KV): Promise<void> {
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
    // The hub's list of the chats is asked alongside me and may have come first: it went with the emptied stores,
    // and with every machine off nothing else draws the chats. It is asked anew, as the person me named, never put
    // back from what the page held.
    forgetCentralList();
    void refreshCentral();
  }
  writeHint(person);
  stopOutbox = startOutbox();
  stopDraftSync = startDraftSync();
  // the person's new chats that wait in the hub for a machine, on every device of theirs.
  stopQueuedChats?.();
  stopQueuedChats = watchQueuedChats();
}

/** me is asked again after 1, 2, 4… up to 30 s, and at once when the browser is back online. */
export const ME_RETRY_MS = [1000, 2000, 4000, 8000, 15000, 30000] as const;

let stopMeRetry: (() => void) | null = null;

function retryMe(mine: number, openKV: (person: string) => KV): void {
  stopMeRetry?.();
  const clock = pageClock();
  let attempt = 0;
  let timer: unknown = null;
  let busy = false;
  let done = false;
  const stop = () => {
    done = true;
    clock.clearTimeout(timer);
    if (typeof window !== 'undefined') window.removeEventListener('online', now);
    if (stopMeRetry === stop) stopMeRetry = null;
  };
  const tryOnce = async () => {
    if (done || busy) return;
    if (mine !== generation) return stop();
    busy = true;
    clock.clearTimeout(timer);
    const me = await loadMe();
    busy = false;
    if (done || mine !== generation) return stop();
    if (me.k === 'ready') {
      stop();
      await adopt(me, openKV);
      return;
    }
    if (me.k === 'signed-out') {
      stop();
      signOut(me);
      return;
    }
    schedule();
  };
  function now() {
    void tryOnce();
  }
  const schedule = () => {
    const ms = ME_RETRY_MS[Math.min(attempt, ME_RETRY_MS.length - 1)]!;
    attempt += 1;
    timer = clock.setTimeout(now, ms);
  };
  if (typeof window !== 'undefined') window.addEventListener('online', now);
  stopMeRetry = stop;
  schedule();
}

/** Starts v2 once per page: cache of the last person → me → (their outbox) → machines → sockets (each machine's
 *  /hello behind them). */
export function bootNext(o: BootOptions = {}): Promise<BootState> {
  const mine = generation;
  const gone = () => mine !== generation;
  booted ??= (async () => {
    if (typeof window !== 'undefined') {
      window.addEventListener('pagehide', flushOnHide);
      document.addEventListener('visibilitychange', flushOnHidden);
      // The machine's journal of what the page does and where it fails.
      stopJournal = startJournal();
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
    // The person's machines are asked alongside me: one after the other cost the first screen a round trip.
    const machinesAsked = loadMachines(gone);
    const meAsked = loadMe();
    const me = await Promise.race([
      meAsked,
      new Promise<MeState>((r) => pageClock().setTimeout(() => r({ k: 'loading' }), ME_FIRST_SCREEN_MS)),
    ]);
    if (gone()) return bootStore.get();
    if (me.k === 'signed-out') return signOut(me);
    if (me.k === 'ready') await adopt(me, openKV);
    else if (me.k === 'loading') {
      // me is late: the machines draw now, and the answer, when it comes, is taken as the first one would have been.
      void meAsked.then(async (late) => {
        if (mine !== generation) return;
        if (late.k === 'ready') await adopt(late, openKV);
        else if (late.k === 'signed-out') signOut(late);
        else retryMe(mine, openKV);
      });
    }
    // me did not answer as it should (a runtime restarting, a reply that is not a record): the machines still load,
    // and me is asked again until it answers; its person's outbox starts then, without a reload.
    else retryMe(mine, openKV);
    const machines = await machinesAsked;
    if (gone()) return bootStore.get();
    if (machines === 'signed-out') return set({ k: 'signed-out' });
    if (!Array.isArray(machines)) return set({ k: 'failed', code: machines.code });
    batch(() => {
      putMachines(machines);
      // the limits the hub remembers draw before any machine answers.
      for (const m of machines) if (m.usage !== undefined) seedLimits(m.id, m.usage);
    });
    if (o.hub) hub.configure(o.hub);
    // Every machine's socket opens at once; /hello is asked of each on its own, behind them. Waiting for every
    // machine's answer held the page up to 10 s on one machine that was off.
    const preferred = me.k === 'ready' ? me.me.settings.defaultMachine : null;
    const all = machines.map((m) => m.id);
    hub.start(all, pickActive(machines, preferred));
    for (const m of machines) {
      // A machine the list already calls unreachable is not asked: its socket says when it is back.
      if (m.reach === 'unreachable') continue;
      void checkRuntime(m.id).then((v2) => {
        if (v2 || gone()) return;
        dropOutdated(m.id, preferred);
      });
    }
    if (fromHub(machines)) followFleet(o.fleetSource);
    return set({ k: 'ready', machines: all });
  })();
  return booted;
}

/**
 * The list is a hub's, not a runtime's own record of itself: a hub says of every machine whose it is (`mine`), and
 * only a hub can list no machine at all. A runtime on its own is one machine for the life of the page, so there is
 * nothing to follow and no stream to ask it for.
 */
const fromHub = (list: readonly FleetMachine[]): boolean =>
  list.length === 0 || list.some((m) => m.mine !== undefined);

/** Follows the hub's fleet stream, and opens it again when a machine's socket comes back: the hub answers again. */
function followFleet(source?: (url: string) => FleetSource | null): void {
  stopFollowingFleet();
  const watch = watchFleet({
    url: clusterApi('/machines/events'),
    refresh: () => void refreshFleet(),
    source,
  });
  fleetWatch = watch;
  stopFleetNudge = connectionStore.subscribeAll((changed) => {
    for (const m of changed) if (connectionStore.get(m)?.k === 'live') return watch.nudge();
  });
}

function stopFollowingFleet(): void {
  fleetWatch?.stop();
  fleetWatch = null;
  stopFleetNudge?.();
  stopFleetNudge = null;
}

let fleetReading = false;
let fleetAgain = false;

/**
 * Reads the person's machines again and brings the open page to the answer: one reading at a time, and a request that
 * comes during one is one more reading after it. A reading that fails changes nothing: the list the page has stays.
 */
export async function refreshFleet(): Promise<void> {
  if (fleetReading) {
    fleetAgain = true;
    return;
  }
  const mine = generation;
  fleetReading = true;
  try {
    do {
      fleetAgain = false;
      const res = await getPage(clusterApi('/machines'), (v) => v as FleetMachine[], 10000);
      if (mine !== generation) return;
      if ('ok' in res && Array.isArray(res.value)) applyFleet(res.value);
    } while (fleetAgain && mine === generation);
  } finally {
    fleetReading = false;
  }
}

/**
 * The difference between the machines the page holds and a new reading of the list. A machine that is new gets its
 * socket (and its /hello behind it); one that is gone has its socket closed and leaves the list; one that was renamed
 * shows its new name. The machines the page already has keep their sockets and everything those said.
 */
export function applyFleet(list: FleetMachine[]): void {
  const st = bootStore.get();
  if (st.k !== 'ready') return;
  const mine = generation;
  const me = meStore.get();
  const preferred = me.k === 'ready' ? me.me.settings.defaultMachine : null;
  const listed = new Set(list.map((m) => m.id));
  const had = new Set(st.machines);
  const outdated = (m: MachineId) => machinesStore.get(m)?.runtime === 'outdated';
  // A machine the page left as an older runtime is asked again below, and joins when it answers v2.
  const added = list.filter((m) => !had.has(m.id) && !outdated(m.id));
  const again = list.filter((m) => !had.has(m.id) && outdated(m.id));
  const removed = st.machines.filter((m) => !listed.has(m));
  const machines = [...st.machines.filter((m) => listed.has(m)), ...added.map((m) => m.id)];
  const next = pickActive(
    list.filter((m) => machines.includes(m.id)),
    preferred,
  );
  batch(() => {
    const forgotten = [...machinesStore.keys()].filter((m) => !listed.has(m));
    mergeMachines(list, (m) => connectionStore.get(m)?.k === 'live');
    for (const m of added) if (m.usage !== undefined) seedLimits(m.id, m.usage);
    for (const m of removed) hub.drop(m, next);
    for (const m of forgotten) machinesStore.delete(m);
    for (const m of added) hub.add(m.id, next);
    if (added.length > 0 || removed.length > 0) set({ k: 'ready', machines });
  });
  for (const m of added) {
    if (m.reach === 'unreachable') continue;
    void checkRuntime(m.id).then((v2) => {
      if (!v2 && mine === generation) dropOutdated(m.id, preferred);
    });
  }
  for (const m of again) {
    if (m.reach === 'unreachable') continue;
    void checkRuntime(m.id).then(() => {
      if (mine === generation && machinesStore.get(m.id)?.runtime === 'v2') joinFleet(m.id, preferred);
    });
  }
}

/** A machine that answered v2 after the page had left it as an older runtime joins the v2 machines. */
function joinFleet(m: MachineId, preferred: MachineId | null): void {
  const st = bootStore.get();
  if (st.k !== 'ready' || st.machines.includes(m)) return;
  const machines = [...st.machines, m];
  batch(() => {
    const views = machines.map((id) => machinesStore.get(id)?.machine ?? ({ id } as Machine));
    hub.add(m, pickActive(views, preferred));
    set({ k: 'ready', machines });
  });
}

/** The machine the page opens on: the person's pick, else the first of their own (#57: not one they only administer),
 *  else the first. */
export const pickActive = (machines: readonly Machine[], preferred: MachineId | null): MachineId | null =>
  preferred && machines.some((m) => m.id === preferred)
    ? preferred
    : (machines.find((m) => m.mine !== false)?.id ?? machines[0]?.id ?? null);

/** A machine whose /hello says it is an older runtime leaves the v2 machines: its socket closes. */
function dropOutdated(m: MachineId, preferred: MachineId | null): void {
  const st = bootStore.get();
  if (st.k !== 'ready' || !st.machines.includes(m)) return;
  const rest = st.machines.filter((id) => id !== m);
  batch(() => {
    const views = rest.map((id) => machinesStore.get(id)?.machine ?? ({ id } as Machine));
    hub.drop(m, pickActive(views, preferred));
    set({ k: 'ready', machines: rest });
  });
}

/** The page lets this person go (sign-out, another account): sockets, outbox, cache and stores. */
export async function shutdownNext(): Promise<void> {
  const p = persistence;
  persistence = null;
  stopOutbox?.();
  stopOutbox = null;
  stopDraftSync?.();
  stopDraftSync = null;
  stopQueuedChats?.();
  stopQueuedChats = null;
  queuedChatsStore.set(null);
  stopJournal?.();
  stopJournal = null;
  stopFollowingFleet();
  fleetAgain = false;
  if (typeof window !== 'undefined') {
    window.removeEventListener('pagehide', flushOnHide);
    document.removeEventListener('visibilitychange', flushOnHidden);
  }
  booted = null;
  generation += 1;
  stopMeRetry?.();
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
