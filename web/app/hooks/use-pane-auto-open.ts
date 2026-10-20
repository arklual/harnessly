import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useSyncExternalStore } from 'react';
import { usePaneSources } from '@/hooks/use-pane-data';
import { currentMachineId } from '@/lib/app-path';
import { readMachineLoad } from '@/lib/hl-machine-load-store';
import { autoOpenAllowed, effectiveLevel } from '@/lib/hl-machine-load-view';
import {
  docCandidates,
  planAutoOpen,
  readAutoOpenSetting,
  serverCandidates,
  standCandidates,
  writeAutoOpenSetting,
  type IAutoCandidate,
} from '@/lib/hl-pane-auto';
import { paneKeyOf, readPane, updatePane } from '@/lib/hl-pane-store';
import { activePaneTab, openTab, type TPaneTabKind } from '@/lib/hl-pane-tabs';
import { markArrival } from '@/lib/hl-pane-arrival';
import { extractLocalUrls, extractStandUrls, previewWorkRoots } from '@/lib/hl-preview-chat-urls';
import { fetchPreviewPorts } from '@/lib/hl-preview-client';
import { readPreview } from '@/lib/hl-preview-store';
import type { ITab } from '@/types/terminal';

/** A dev server listens before it compiles: quick looks for the first minute, slower ones for four more. */
const PROBE_QUICK = { count: 30, ms: 2_000 } as const;
const PROBE_SLOW = { count: 48, ms: 5_000 } as const;

/** Tabs the person is not doing anything in: a new document may take their place in front. */
const IDLE_KINDS: readonly TPaneTabKind[] = ['overview', 'tools'];

const settingListeners = new Set<() => void>();

const browserStorage = (): Storage | null => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

const subscribeSetting = (listener: () => void): (() => void) => {
  settingListeners.add(listener);
  window.addEventListener('storage', listener);
  return () => {
    settingListeners.delete(listener);
    window.removeEventListener('storage', listener);
  };
};

/** Whether the panel opens new documents, boards and dev servers by itself on this device. */
export const usePaneAutoSetting = (): [boolean, (enabled: boolean) => void] => {
  const enabled = useSyncExternalStore(
    subscribeSetting,
    () => readAutoOpenSetting(browserStorage()),
    () => true,
  );
  const change = useCallback((next: boolean) => {
    writeAutoOpenSetting(browserStorage(), next);
    for (const listener of settingListeners) listener();
  }, []);
  return [enabled, change];
};

const wait = (ms: number, signal: AbortSignal): Promise<void> =>
  new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener(
      'abort',
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });

/** True once the port answers as a site; false for an API, a server that never came up, or a conversation left meanwhile. */
const answersAsSite = async (
  port: number,
  roots: readonly string[],
  signal: AbortSignal,
): Promise<boolean> => {
  for (let attempt = 0; attempt < PROBE_QUICK.count + PROBE_SLOW.count && !signal.aborted; attempt++) {
    const found = (await fetchPreviewPorts(roots, signal).catch(() => null))?.find(
      (row) => row.port === port,
    );
    if (found?.responding && found.kind === 'site') return !signal.aborted;
    if (found?.responding && found.kind === 'api') return false;
    await wait(attempt < PROBE_QUICK.count ? PROBE_QUICK.ms : PROBE_SLOW.ms, signal);
  }
  return false;
};

interface IPaneAutoOpenOptions {
  mode: 'agent' | 'chat';
  tab: ITab | null;
  chatId: string | null;
  /** The conversation is on screen: a hidden one opens nothing. */
  active: boolean;
  paneVisible: boolean;
  /** A phone, where the panel is a sheet over the whole chat: it is not thrown over what the person reads. */
  overlay: boolean;
  onShow: () => void;
}

/**
 * A document or a board the agent has just published and the dev server it has just started open as tabs of
 * the right panel by themselves, and the panel shows up for them. A person busy in another tab gets the new one behind,
 * marked unread; what opened once never opens by itself again.
 */
const usePaneAutoOpen = ({
  mode,
  tab,
  chatId,
  active,
  paneVisible,
  overlay,
  onShow,
}: IPaneAutoOpenOptions): void => {
  const [enabled] = usePaneAutoSetting();
  const key = paneKeyOf(mode, tab?.id ?? null, chatId);
  const { links, entries, workPaths, cwd, jsonlPath, loaded, complete } = usePaneSources(mode, tab, chatId);
  const ready = active && key !== null && loaded && (mode === 'chat' || !jsonlPath || complete);
  const urls = useMemo(() => (mode === 'agent' ? extractLocalUrls(entries) : []), [mode, entries]);
  const stands = useMemo(() => (mode === 'agent' ? extractStandUrls(entries) : []), [mode, entries]);
  const candidates = useMemo(
    () => [...docCandidates(links), ...serverCandidates(urls), ...standCandidates(stands)],
    [links, urls, stands],
  );
  const roots = useMemo(() => previewWorkRoots(workPaths, cwd), [workPaths, cwd]);
  const probes = useRef(new Map<string, AbortController>());

  const place = useEffectEvent((paneKey: string, opened: readonly IAutoCandidate[]) => {
    if (paneKey !== key || opened.length === 0) return;
    const busy = paneVisible && !IDLE_KINDS.includes(activePaneTab(readPane(paneKey)).kind);
    for (const { request } of opened) markArrival(paneKey, request.kind, request.target ?? null);
    updatePane(paneKey, (state) =>
      opened.reduce(
        (next, { request }) =>
          openTab(next, request.kind, request.target ?? null, { title: request.title, background: busy }),
        state,
      ),
    );
    if (!paneVisible && !overlay) onShow();
  });

  const watchServer = useEffectEvent((paneKey: string, candidate: IAutoCandidate) => {
    const { port } = candidate;
    const tabId = tab?.id;
    if (port === undefined || !tabId || probes.current.has(candidate.id)) return;
    const controller = new AbortController();
    probes.current.set(candidate.id, controller);
    void answersAsSite(port, roots, controller.signal).then((up) => {
      probes.current.delete(candidate.id);
      if (!up || !autoOpenAllowed(effectiveLevel(readMachineLoad(currentMachineId() ?? '')))) return;
      // A page the person (or an earlier server) already has in the browser is theirs: it is not replaced.
      const { state } = readPreview(tabId);
      if (state.kind === 'open' || state.kind === 'opening' || state.kind === 'taken') return;
      place(paneKey, [candidate]);
    });
  });

  // A stand the agent named opens like a server it started: unless the person already has a page in the browser.
  const placeStand = useEffectEvent((paneKey: string, candidate: IAutoCandidate) => {
    const tabId = tab?.id;
    if (!tabId) return;
    const { state } = readPreview(tabId);
    if (state.kind === 'open' || state.kind === 'opening' || state.kind === 'taken') return;
    place(paneKey, [candidate]);
  });

  useEffect(() => {
    const running = probes.current;
    return () => {
      for (const controller of running.values()) controller.abort();
      running.clear();
    };
  }, [key]);

  useEffect(() => {
    if (!ready || !key) return;
    const plan = planAutoOpen(readPane(key).seen, candidates, Date.now());
    if (!plan) return;
    updatePane(key, (state) => ({ ...state, seen: plan.seen }));
    if (!enabled) return;
    place(
      key,
      plan.open.filter((candidate) => candidate.port === undefined && !candidate.stand),
    );
    const server = plan.open.filter((candidate) => candidate.port !== undefined).at(-1);
    if (server) watchServer(key, server);
    const stand = plan.open.filter((candidate) => candidate.stand).at(-1);
    if (stand) placeStand(key, stand);
  }, [ready, key, candidates, enabled]);
};

export default usePaneAutoOpen;
