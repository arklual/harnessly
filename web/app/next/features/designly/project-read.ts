// One project opened from wherever it can be had. The machine is asked at once; the hub's store is asked
// when the machine is slow, has failed or does not have the project. A store that cannot give the project (no copy,
// a failed read) never ends the wait for the machine: the project then opens from the machine and the page asks the
// machine to put it into the store (pushToStore), so the next look reads the hub. The card «not here» is the answer
// only when neither side gave the project, and it names why.
import { useEffect, useRef } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { WordedError } from '@/next/core/net/worded';
import {
  storeIsOff,
  type IDesign,
  type IDesignLibrary,
  type IStoreLibrary,
} from '@/next/features/designly/api';
import type { MachineId } from '@/next/protocol/v2';

export const PROJECT_FROM = { machine: 'machine', store: 'store' } as const;

/** Why the project is on neither side: what the machine said and what the store said. */
export const AWAY = {
  /** The machine did not answer (off, hung, an error) and the store has no copy. */
  silentNoCopy: 'silentNoCopy',
  /** The machine did not answer and the store could not be read. */
  silentUnreadable: 'silentUnreadable',
  /** The machine answered without the project and the store has no copy. */
  absentNoCopy: 'absentNoCopy',
  /** The machine answered without the project and the store could not be read. */
  absentUnreadable: 'absentUnreadable',
  /** The machine did not answer and the hub keeps no store (HUB_STORE off): only the machine can give it. */
  silentStoreOff: 'silentStoreOff',
  /** The machine answered without the project and the hub keeps no store: nothing will bring it here (GitHub #82). */
  absentStoreOff: 'absentStoreOff',
  /** The machine refused the person outright (403): the project is on a machine they may not open (GitHub #82). */
  machineDenied: 'machineDenied',
} as const;
export type TAway = (typeof AWAY)[keyof typeof AWAY];

/** Reasons nothing on this page can change: the card stays as it is and the library is not read again. */
export const awayIsFinal = (why: TAway | null): boolean =>
  why === AWAY.absentStoreOff || why === AWAY.machineDenied;

/** Reasons the machine is the one awaited: it has been waited for already, the next look leaves it the same time. */
export const awayIsSilent = (why: TAway | null): boolean =>
  why === AWAY.silentNoCopy || why === AWAY.silentUnreadable || why === AWAY.silentStoreOff;

export const PROJECT_READ = { open: 'open', away: 'away' } as const;

export type TProjectRead =
  | { k: typeof PROJECT_READ.open; from: typeof PROJECT_FROM.machine; library: IDesignLibrary }
  | { k: typeof PROJECT_READ.open; from: typeof PROJECT_FROM.store; design: IDesign }
  | {
      k: typeof PROJECT_READ.away;
      why: TAway;
      /** The machine's own error when it failed outright, for a runtime with no hub behind it. */
      machineError: string | null;
    };

/** How long the machine is waited for after the store turned out to be of no help, from the start of the read, ms. */
export const MACHINE_PATIENCE_MS = 20_000;

const SIDE = {
  pending: 'pending',
  has: 'has',
  absent: 'absent',
  failed: 'failed',
  off: 'off',
  denied: 'denied',
} as const;
type TSide = (typeof SIDE)[keyof typeof SIDE];

const WHY: Record<'silent' | 'absent', Record<'absent' | 'failed' | 'off', TAway>> = {
  silent: { absent: AWAY.silentNoCopy, failed: AWAY.silentUnreadable, off: AWAY.silentStoreOff },
  absent: { absent: AWAY.absentNoCopy, failed: AWAY.absentUnreadable, off: AWAY.absentStoreOff },
};

export interface IProjectReadWaits {
  /** The machine's head start before the store is asked too. */
  store: number;
  /** The longest the machine is waited for once the store has nothing. */
  machine: number;
}

/**
 * Reads project `id`. `show` gets every state worth showing, in order: the store's copy while the machine is slow, the
 * machine's project when it comes (it replaces the copy), or «away» with its reason. The promise settles on the first.
 * Nothing is aborted by a wait running out: a machine that answers late still opens the project.
 */
export const readProject = (
  id: string,
  read: { machine: () => Promise<IDesignLibrary>; store: () => Promise<IStoreLibrary> },
  show: (state: TProjectRead) => void,
  waits: IProjectReadWaits,
): Promise<void> =>
  new Promise((resolve) => {
    let machine: TSide = SIDE.pending;
    let store: TSide = SIDE.pending;
    let storeAsked = false;
    let machineError: string | null = null;
    let patient = true;
    const put = (state: TProjectRead) => {
      show(state);
      resolve();
    };
    // Away is said once both sides have spoken, or the machine has been waited for long enough.
    const settle = () => {
      if (machine === SIDE.has || store === SIDE.has || store === SIDE.pending) return;
      if (machine === SIDE.pending && patient) return;
      if (machine === SIDE.denied) {
        put({ k: PROJECT_READ.away, why: AWAY.machineDenied, machineError });
        return;
      }
      const machineSaid = machine === SIDE.absent ? 'absent' : 'silent';
      const storeSaid = store === SIDE.off ? 'off' : store === SIDE.absent ? 'absent' : 'failed';
      put({ k: PROJECT_READ.away, why: WHY[machineSaid][storeSaid], machineError });
    };
    const askStore = () => {
      if (storeAsked || machine === SIDE.has) return;
      storeAsked = true;
      read.store().then(
        (copy) => {
          const design = copy.designs.find((d) => d.id === id);
          store = design ? SIDE.has : SIDE.absent;
          // The machine's project, once shown, is never replaced by the copy.
          if (design && machine !== SIDE.has) put({ k: PROJECT_READ.open, from: PROJECT_FROM.store, design });
          settle();
        },
        (failure: unknown) => {
          store = storeIsOff(failure) ? SIDE.off : SIDE.failed;
          settle();
        },
      );
    };
    const storeTimer = setTimeout(askStore, waits.store);
    const patience = setTimeout(() => {
      patient = false;
      settle();
    }, waits.machine);
    read.machine().then(
      (library) => {
        clearTimeout(patience);
        if (library.designs.some((d) => d.id === id)) {
          clearTimeout(storeTimer);
          machine = SIDE.has;
          put({ k: PROJECT_READ.open, from: PROJECT_FROM.machine, library });
          return;
        }
        machine = SIDE.absent;
        askStore();
        settle();
      },
      (failure: unknown) => {
        clearTimeout(patience);
        machine = failure instanceof WordedError && failure.status === 403 ? SIDE.denied : SIDE.failed;
        machineError = failure instanceof Error ? failure.message : String(failure);
        askStore();
        settle();
      },
    );
  });

/**
 * The reads of a project on one page. A quiet re-read (the pages' poll while an agent works, every few seconds) used to
 * abort the read before it: on a loaded machine the library takes longer than the poll, so the machine never answered,
 * the store always did, and a design that had once fallen back to the snapshot (a runtime restarting answers 503) stayed
 * on it, read-only, with «Редактирование» locked. Now a re-read of the same project leaves the one before to
 * finish, and none starts while the machine is still answering one; another project, machine or attempt aborts them.
 */
export const createProjectReads = () => {
  let key: string | null = null;
  // The machine's reads on their way, of this key only: a read of an earlier key may still settle after it is aborted.
  let generation = 0;
  let machineBusy = 0;
  const running = new Set<AbortController>();
  const stop = () => {
    for (const abort of running) abort.abort();
    running.clear();
    key = null;
    generation += 1;
    machineBusy = 0;
  };
  return {
    /** A read for `next`: its signal, or null when the machine is still answering a read of the same project. */
    start: (next: string): AbortSignal | null => {
      if (next === key && machineBusy > 0) return null;
      if (next !== key) stop();
      key = next;
      const abort = new AbortController();
      running.add(abort);
      return abort.signal;
    },
    /** The machine's read, counted while it is on its way. */
    machine: <T>(read: () => Promise<T>): Promise<T> => {
      const mine = generation;
      machineBusy += 1;
      return read().finally(() => {
        if (mine === generation) machineBusy -= 1;
      });
    },
    stop,
  };
};

/** Asks the machine to put the project into the hub's store now (design.StorePush); the sync does the sending. */
export const pushToStore = (m: MachineId, project: string): Promise<unknown> =>
  runtimeJson<unknown>(m, platformPath('/design/store-push'), postJson({ project }));

/**
 * A project open from its machine that the hub's store has nothing of (`lacks`) is put into the store at once, so the
 * next look reads the hub. Once per project; the sync would get there by itself, only later.
 */
export const useStorePush = (m: MachineId, project: string, lacks: boolean): void => {
  const pushed = useRef<string | null>(null);
  useEffect(() => {
    const key = `${m}|${project}`;
    if (!lacks || pushed.current === key) return;
    pushed.current = key;
    // A runtime with no store refuses (409): the design stays open from the machine, as it is.
    pushToStore(m, project).catch(() => undefined);
  }, [m, project, lacks]);
};
