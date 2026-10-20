import { useCallback, useEffect } from 'react';
import { create } from 'zustand';
import { createSharedResource, type ISharedResource } from '@/lib/shared-resource';
import { APP_BASE_PATH } from '@/lib/app-path';
import type { IOrganizer, TOrganizerOp } from '@/lib/hl-organizer';
import useSharedResource from '@/hooks/use-shared-resource';
import { t } from '@/lib/i18n';

/**
 * The organizer of the signed-in user. A plain fetch without `?machine=` on purpose:
 * the fleet gateway sends it to the default machine, while appFetch inside a runtime iframe would pin
 * it to that iframe's machine.
 */
const ORGANIZER_URL = `${APP_BASE_PATH}/api/platform/organizer`;

const POLL = { intervalMs: 10_000, maxBackoffMs: 2 * 60_000 } as const;

/** What the sidebar is told when the default machine does not answer; it says so in its own words around it. */
export const ORGANIZER_MACHINE_DOWN = t('hooks.useHlOrganizer.machineDown');

const ERROR_TEXT: Record<string, string> = {
  'organizer-unreadable': t('hooks.useHlOrganizer.unreadable'),
  'project-not-found': t('hooks.useHlOrganizer.projectNotFound'),
  'project-exists': t('hooks.useHlOrganizer.projectExists'),
  'invalid-op': t('hooks.useHlOrganizer.invalidOp'),
  Forbidden: t('hooks.useHlOrganizer.forbidden'),
};

const LIMIT_TEXT: Record<string, string> = {
  pins: t('hooks.useHlOrganizer.limitPins'),
  projects: t('hooks.useHlOrganizer.limitProjects'),
  assignments: t('hooks.useHlOrganizer.limitAssignments'),
};

class OrganizerRequestError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

const request = async (init?: RequestInit): Promise<IOrganizer> => {
  let response: Response;
  try {
    response = await fetch(ORGANIZER_URL, { cache: 'no-store', credentials: 'same-origin', ...init });
  } catch (error) {
    if ((error as Error).name === 'AbortError') throw error;
    throw new OrganizerRequestError(0, 'network', ORGANIZER_MACHINE_DOWN);
  }
  const body = (await response.json().catch(() => null)) as {
    organizer?: IOrganizer;
    error?: string;
    what?: string;
  } | null;
  if (response.ok && body?.organizer) return body.organizer;
  const code = body?.error ?? `http-${response.status}`;
  const text = (code === 'limit' && body?.what && LIMIT_TEXT[body.what]) || ERROR_TEXT[code];
  // 502/504 come from the gateway when the default machine is down; a code means nothing to the person.
  throw new OrganizerRequestError(response.status, code, text ?? ORGANIZER_MACHINE_DOWN);
};

const organizerSource: ISharedResource<IOrganizer> = createSharedResource<IOrganizer>({
  load: async (signal): Promise<IOrganizer> => {
    const loaded = await request({ signal });
    // A PATCH that finished while this GET was in flight already holds a newer revision.
    const current = organizerSource.getSnapshot().data;
    return current && current.revision > loaded.revision ? current : loaded;
  },
  describeError: (error) =>
    error instanceof Error ? error.message : t('hooks.useHlOrganizer.machineNoAnswer'),
  intervalMs: POLL.intervalMs,
  maxBackoffMs: POLL.maxBackoffMs,
});

interface IPendingStore {
  keys: ReadonlySet<string>;
  add: (keys: readonly string[]) => void;
  remove: (keys: readonly string[]) => void;
}

/** Rows and projects with a request in flight: their triggers show a spinner and ignore presses. */
const usePendingStore = create<IPendingStore>((set) => ({
  keys: new Set(),
  add: (keys) => set((state) => ({ keys: new Set([...state.keys, ...keys]) })),
  remove: (keys) => set((state) => ({ keys: new Set([...state.keys].filter((key) => !keys.includes(key))) })),
}));

export type TOrganizerApplyResult = { ok: true; organizer: IOrganizer } | { ok: false; error: string };

/**
 * Pessimistic: nothing changes on screen until the server answers with the whole new organizer.
 * `keys` mark what is waiting (a ref, a project id) for spinners.
 */
export const applyOrganizerOps = async (
  ops: TOrganizerOp[],
  keys: readonly string[] = [],
): Promise<TOrganizerApplyResult> => {
  const pending = usePendingStore.getState();
  pending.add(keys);
  try {
    const organizer = await request({
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ops }),
    });
    organizerSource.update((current) =>
      current && current.revision > organizer.revision ? current : organizer,
    );
    return { ok: true, organizer };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : t('hooks.useHlOrganizer.saveFailed'),
    };
  } finally {
    usePendingStore.getState().remove(keys);
  }
};

export const refreshOrganizer = () => organizerSource.refresh();

/** `organizer` is null while loading and while the default machine has never answered; then `error` says why. */
const useHlOrganizer = () => {
  const state = useSharedResource(organizerSource);
  const pendingKeys = usePendingStore((s) => s.keys);

  useEffect(() => {
    const onFocus = () => void organizerSource.refresh();
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, []);

  const apply = useCallback(
    (ops: TOrganizerOp[], keys?: readonly string[]) => applyOrganizerOps(ops, keys),
    [],
  );

  return { organizer: state.data, error: state.error, pendingKeys, apply, refresh: refreshOrganizer };
};

export default useHlOrganizer;
