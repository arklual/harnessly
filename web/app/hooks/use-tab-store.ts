// Each tab's live state from the runtime's status socket, plus what the page learned about its session.
import { create } from 'zustand';
import type { IStatusEntry } from '@/types/status';
import type { TCliState } from '@/types/timeline';

export interface ISessionMeta {
  model: string | null;
  effort: string | null;
  usedPercentage?: number | null;
  currentContextTokens?: number | null;
  contextWindowSize?: number | null;
  [k: string]: unknown;
}

export interface ISessionMetaCache {
  sessionId: string | null;
  jsonlPath: string | null;
  meta: ISessionMeta;
}

export interface ITabState extends Omit<IStatusEntry, 'cliState'> {
  cliState: TCliState;
  sessionMetaCache?: ISessionMetaCache | null;
}

const stateOf = (entry: IStatusEntry, previous?: ITabState): ITabState => ({
  ...entry,
  cliState: entry.cliState ?? 'unknown',
  sessionMetaCache: previous?.sessionMetaCache ?? null,
});

interface ITabStore {
  tabs: Record<string, ITabState>;
  replaceAll: (tabs: Record<string, IStatusEntry>) => void;
  upsert: (tabId: string, entry: IStatusEntry | null) => void;
  patch: (tabId: string, part: Partial<ITabState>) => void;
}

const useTabStore = create<ITabStore>((set) => ({
  tabs: {},
  replaceAll: (incoming) =>
    set((s) => {
      const tabs: Record<string, ITabState> = {};
      for (const [id, entry] of Object.entries(incoming)) tabs[id] = stateOf(entry, s.tabs[id]);
      return { tabs };
    }),
  upsert: (tabId, entry) =>
    set((s) => {
      const tabs = { ...s.tabs };
      if (!entry) delete tabs[tabId];
      else tabs[tabId] = stateOf(entry, s.tabs[tabId]);
      return { tabs };
    }),
  patch: (tabId, part) =>
    set((s) => {
      const cur = s.tabs[tabId];
      if (!cur) return s;
      return { tabs: { ...s.tabs, [tabId]: { ...cur, ...part } } };
    }),
}));

/** Tabs that wait for the person (a question, or finished work not yet looked at). */
export const selectGlobalStatus = (tabs: Record<string, ITabState>) => ({
  attentionCount: Object.values(tabs).filter(
    (t) => t.cliState === 'needs-input' || (t.cliState === 'ready-for-review' && !t.dismissedAt),
  ).length,
});

export default useTabStore;
