import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import usePoller from '@/hooks/use-poller';
import { useHlMachines } from '@/hooks/use-hl-machines';
import { currentMachineId } from '@/lib/app-path';
import { fetchDesignLibrary } from '@/lib/hl-design-library-client';
import { mergeLibraries, type ILibraryProject, type IMachineLibrary } from '@/lib/hl-design-library/fleet';
import { parseLibraryPagePath, type ILibraryResponse } from '@/lib/hl-design-library/types';
import { t } from '@/lib/i18n';

/** The library while its tab or the overview is in front, and rarely otherwise: the counts stay close. */
const POLL = { frontMs: 5_000, behindMs: 30_000, maxBackoffMs: 60_000 } as const;

export type TDesignLibraryState =
  { kind: 'loading' } | { kind: 'error'; message: string } | { kind: 'ready'; data: ILibraryResponse };

const portListeners = new Set<() => void>();
let knownPort: number | null = null;
let portRequest: Promise<number | null> | null = null;

// Ports of the library servers any machine named: a board of another machine in the preview is a library page too.
const libraryPorts = new Set<number>();

const rememberPort = (port: number) => {
  libraryPorts.add(port);
  if (knownPort === port) return;
  knownPort = port;
  for (const listener of portListeners) listener();
};

/** The library server's port on this page's machine once any request told it; null before. */
export const knownLibraryPort = (): number | null => knownPort;

/** Whether a dev-server port in a preview is the library server of this page's machine or of another one. */
export const isLibraryPort = (port: number): boolean => port === knownPort || libraryPorts.has(port);

/** Asks the runtime for the port once (`?lite=1`, no list): the browser tab and the element picker need only that. */
export const ensureLibraryPort = (): Promise<number | null> => {
  if (knownPort !== null) return Promise.resolve(knownPort);
  portRequest ??= fetchDesignLibrary(currentMachineId(), null, { lite: true })
    .then(
      (data) => {
        rememberPort(data.port);
        return data.port;
      },
      () => null,
    )
    .finally(() => {
      portRequest = null;
    });
  return portRequest;
};

const subscribePort = (listener: () => void) => {
  portListeners.add(listener);
  return () => {
    portListeners.delete(listener);
  };
};

export const useLibraryPort = (): number | null =>
  useSyncExternalStore(subscribePort, knownLibraryPort, () => null);

// Titles of the designs and boards the last list named, for the address bar to show a board by its name.
const titles = new Map<string, string>();
const rememberTitles = (data: ILibraryResponse) => {
  for (const artifact of data.artifacts) {
    titles.set(artifact.id, artifact.title);
    for (const board of artifact.boards) titles.set(`${artifact.id}\n${board.path}`, board.title);
  }
};

const stem = (file: string): string => (file.split('/').pop() ?? file).replace(/\.dc\.html$|\.html?$/, '');

/** "Design · board" for a page of the library server (`/a/<id>/p/<file>?…`); null for any other page. */
export const libraryPageLabel = (pagePath: string): string | null => {
  const page = parseLibraryPagePath(pagePath);
  if (!page) return null;
  const query = pagePath.includes('?') ? pagePath.slice(pagePath.indexOf('?')).split('#', 1)[0] : '';
  const board =
    titles.get(`${page.ref}\n${page.file}${query}`) ??
    titles.get(`${page.ref}\n${page.file}`) ??
    stem(page.file);
  return `${titles.get(page.ref) ?? page.ref} · ${board}`;
};

/**
 * The design library as one conversation sees it: `tabId` marks its own artifacts. Polls while `enabled`, fast when
 * `front` (its tab or the overview is on screen), and keeps the last list through a failed poll.
 */
const useDesignLibrary = (tabId: string | null, enabled: boolean, front: boolean) => {
  const key = enabled ? `design-library:${tabId ?? ''}` : null;
  const [state, setState] = useState<TDesignLibraryState>({ kind: 'loading' });
  // Another conversation's list must not show under this one while the first request for it is in flight.
  const [shownKey, setShownKey] = useState(key);
  if (shownKey !== key) {
    setShownKey(key);
    setState({ kind: 'loading' });
  }
  const [machine] = useState(() => currentMachineId());
  const { refresh, poke } = usePoller(
    key,
    async (signal) => {
      try {
        const data = await fetchDesignLibrary(machine, tabId, { signal });
        rememberPort(data.port);
        rememberTitles(data);
        // A reply for the previous conversation can land after the switch: its "this agent" marks are someone else's.
        if (!signal.aborted && data.selfTabId === tabId) setState({ kind: 'ready', data });
      } catch (error) {
        if (signal.aborted) return;
        const message = error instanceof Error ? error.message : t('hooks.useDesignLibrary.openFailed');
        setState((prev) => (prev.kind === 'ready' ? prev : { kind: 'error', message }));
        throw error;
      }
    },
    { intervalMs: () => (front ? POLL.frontMs : POLL.behindMs), maxBackoffMs: POLL.maxBackoffMs },
  );
  // Coming to the front asks at once instead of waiting out the slow interval.
  useEffect(() => {
    if (front) poke();
  }, [front, poke]);
  return { state, refresh };
};

export type TDesignLibrary = ReturnType<typeof useDesignLibrary>;

/** A machine that has not answered in this long is shown from its last list and named as not answering. */
const MACHINE_TIMEOUT_MS = 8_000;

interface IFleetTarget {
  machineId: string | undefined;
  machineName: string | null;
  /** The gateway's own view of the machine; one it already knows is down is not asked. */
  available: boolean;
}

export type TFleetLibraryState =
  | { kind: 'loading' }
  | { kind: 'error'; message: string }
  | { kind: 'ready'; projects: ILibraryProject[]; unreachable: string[] };

/**
 * The design library of every machine of the person, as the centre section shows it: each machine has its
 * own library folder, and a person works on several. Machines are asked side by side; one that is slow or down does not
 * hold the others, keeps its last list on screen and is named. Without a fleet (a runtime opened directly) it is the
 * one library of that runtime.
 */
export const useFleetDesignLibrary = () => {
  const machines = useHlMachines();
  const fleet = machines.data;
  const failed = machines.data === null && machines.error !== null;
  const targets = useMemo<IFleetTarget[] | null>(() => {
    if (fleet && fleet.defaultMachineId !== undefined && fleet.machines.length > 0) {
      return fleet.machines.map((machine) => ({
        machineId: machine.id,
        machineName: machine.name || machine.id,
        available: machine.available !== false,
      }));
    }
    // A runtime answering for itself, or no list at all: the page's own library is still worth showing.
    return fleet || failed ? [{ machineId: currentMachineId(), machineName: null, available: true }] : null;
  }, [fleet, failed]);
  const pageMachine = currentMachineId() ?? fleet?.defaultMachineId ?? undefined;
  const key = targets
    ? `design-library:fleet:${targets.map((target) => `${target.machineId ?? ''}${target.available ? '' : '!'}`).join(',')}`
    : null;
  const [state, setState] = useState<TFleetLibraryState>({ kind: 'loading' });
  const kept = useRef(new Map<string, IMachineLibrary>());

  const { refresh } = usePoller(
    key,
    async (signal) => {
      if (!targets) return;
      const answers = await Promise.all(
        targets.map(async (target) => {
          if (!target.available) return { target, data: null };
          try {
            const data = await fetchDesignLibrary(target.machineId, null, {
              signal: AbortSignal.any([signal, AbortSignal.timeout(MACHINE_TIMEOUT_MS)]),
            });
            return { target, data };
          } catch {
            return { target, data: null };
          }
        }),
      );
      if (signal.aborted) return;
      const libraries: IMachineLibrary[] = [];
      const unreachable: string[] = [];
      for (const { target, data } of answers) {
        const slot = target.machineId ?? '';
        if (data) {
          const library = { machineId: target.machineId, machineName: target.machineName, data };
          kept.current.set(slot, library);
          libraries.push(library);
          rememberTitles(data);
          libraryPorts.add(data.port);
          if (target.machineId === pageMachine) rememberPort(data.port);
        } else {
          unreachable.push(target.machineName ?? target.machineId ?? t('hooks.useDesignLibrary.machine'));
          const last = kept.current.get(slot);
          if (last) libraries.push(last);
        }
      }
      if (libraries.length === 0) {
        setState({ kind: 'error', message: t('hooks.useDesignLibrary.noMachineAnswered') });
        throw new Error('design library: no machine answered');
      }
      setState({ kind: 'ready', projects: mergeLibraries(libraries, pageMachine), unreachable });
    },
    { intervalMs: POLL.frontMs, maxBackoffMs: POLL.maxBackoffMs },
  );
  return { state, refresh };
};

export default useDesignLibrary;
