import { useCallback, useEffect, useState, useSyncExternalStore } from 'react';
import usePoller from '@/hooks/use-poller';
import {
  clearDevtools,
  fetchDevtools,
  fetchServerLog,
  patchDevtoolsSettings,
} from '@/lib/hl-preview-devtools-client';
import { applySnapshot, emptyLog, type IDevtoolsLog } from '@/lib/hl-preview-devtools-view';
import type { IDevtoolsSettings, IServerLog } from '@/lib/hl-preview/devtools/types';
import { t } from '@/lib/i18n';

export const DEVTOOLS_TAB = { network: 'network', console: 'console', server: 'server' } as const;
export type TDevtoolsTab = (typeof DEVTOOLS_TAB)[keyof typeof DEVTOOLS_TAB];

interface IDrawerState {
  open: boolean;
  tab: TDevtoolsTab;
}

const CLOSED: IDrawerState = { open: false, tab: DEVTOOLS_TAB.network };

/** Per preview (conversation tab): the drawer stays open when the page moves between the panel and the floating window. */
const drawers = new Map<string, IDrawerState>();
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const patchDevtoolsDrawer = (key: string, patch: Partial<IDrawerState>) => {
  drawers.set(key, { ...(drawers.get(key) ?? CLOSED), ...patch });
  for (const listener of listeners) listener();
};

export const useDevtoolsDrawer = (key: string) => {
  const state = useSyncExternalStore(
    subscribe,
    () => drawers.get(key) ?? CLOSED,
    () => CLOSED,
  );
  const patch = useCallback((next: Partial<IDrawerState>) => patchDevtoolsDrawer(key, next), [key]);
  return [state, patch] as const;
};

const message = (error: unknown, fallback: string) => (error instanceof Error ? error.message : fallback);

/** Open drawer: about once a second, as a log should feel live. Closed: only enough to know the page is slowed. */
const OPEN_INTERVAL_MS = 1_000;
const CLOSED_INTERVAL_MS = 15_000;
const SERVER_INTERVAL_MS = 3_000;

export type TSettingsField = keyof IDevtoolsSettings;

interface IUsePreviewDevtoolsOptions {
  /** The preview slot's origin; null without an open page. */
  slot: string | null;
  machineId?: string;
  open: boolean;
}

/**
 * The slot's requests, console and conditions, polled from the runtime while the page is visible. Settings
 * are pessimistic: the field being saved is reported, and what the runtime answers is what is shown.
 */
export const usePreviewDevtools = ({ slot, machineId, open }: IUsePreviewDevtoolsOptions) => {
  const [stored, setLog] = useState<IDevtoolsLog>(() => emptyLog(slot));
  const log = stored.slot === slot ? stored : emptyLog(slot);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState<TSettingsField | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [clearing, setClearing] = useState(false);
  /** When the last answer came: pending requests are drawn up to it, so a render never reads the clock. */
  const [polledAt, setPolledAt] = useState(0);

  const { refresh } = usePoller(
    slot ? `${slot}\n${machineId ?? ''}` : null,
    async (signal) => {
      if (!slot) return;
      const since = log.cursor;
      let snapshot;
      let restarted = false;
      try {
        snapshot = await fetchDevtools(slot, since, machineId, signal);
        // The runtime started over: what is known belongs to a record that is gone, the new one is read from its start.
        if (snapshot.cursor < since) {
          restarted = true;
          snapshot = await fetchDevtools(slot, 0, machineId, signal);
        }
      } catch (failure) {
        if (signal.aborted) return;
        setError(message(failure, t('hooks.usePreviewDevtools.requestsFailed')));
        throw failure;
      }
      setError(null);
      setPolledAt(Date.now());
      setLog((current) => {
        const base = current.slot === slot && !restarted ? current : emptyLog(slot);
        return applySnapshot(base, snapshot, restarted ? 0 : since).log;
      });
    },
    { intervalMs: open ? OPEN_INTERVAL_MS : CLOSED_INTERVAL_MS, maxBackoffMs: 30_000 },
  );

  useEffect(() => {
    if (open) void refresh();
  }, [open, refresh]);

  /** True when the runtime took the change. */
  const update = async (field: TSettingsField, patch: Partial<IDevtoolsSettings>): Promise<boolean> => {
    if (!slot || saving) return false;
    setSaving(field);
    setSaveError(null);
    try {
      const settings = await patchDevtoolsSettings(slot, patch, machineId);
      setLog((current) =>
        current.slot === slot ? { ...current, settings } : { ...emptyLog(slot), settings },
      );
      return true;
    } catch (failure) {
      setSaveError(message(failure, t('hooks.usePreviewDevtools.settingsFailed')));
      return false;
    } finally {
      setSaving(null);
    }
  };

  const clear = async () => {
    if (!slot || clearing) return;
    setClearing(true);
    setSaveError(null);
    try {
      await clearDevtools(slot, machineId);
      setLog((current) =>
        current.slot === slot ? { ...current, network: [], console: [] } : emptyLog(slot),
      );
    } catch (failure) {
      setSaveError(message(failure, t('hooks.usePreviewDevtools.clearFailed')));
    } finally {
      setClearing(false);
    }
  };

  return {
    log,
    polledAt,
    error,
    saving,
    saveError,
    clearing,
    update,
    clear,
    dismissSaveError: () => setSaveError(null),
  };
};

export type TPreviewDevtools = ReturnType<typeof usePreviewDevtools>;

interface IUseServerLogOptions {
  slot: string | null;
  machineId?: string;
  /** The "Server" tab is on screen. */
  active: boolean;
  all: boolean;
}

/** The dev server's terminal problems, every few seconds while the tab is shown. null until the first answer. */
export const useServerLog = ({ slot, machineId, active, all }: IUseServerLogOptions) => {
  const [log, setLog] = useState<{ key: string; value: IServerLog } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const key = slot ? `${slot}\n${machineId ?? ''}\n${all}` : null;

  usePoller(
    active ? key : null,
    async (signal) => {
      if (!slot || !key) return;
      try {
        const value = await fetchServerLog(slot, all, machineId, signal);
        setLog({ key, value });
        setError(null);
      } catch (failure) {
        if (signal.aborted) return;
        setError(message(failure, t('hooks.usePreviewDevtools.serverOutputFailed')));
        throw failure;
      }
    },
    { intervalMs: SERVER_INTERVAL_MS, maxBackoffMs: 30_000 },
  );

  return { log: log && log.key === key ? log.value : null, error };
};
