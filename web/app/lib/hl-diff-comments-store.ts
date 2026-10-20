import { useSyncExternalStore } from 'react';
import type { IDiffComment } from '@/lib/hl-diff-comment';

export type TNewDiffComment = Omit<IDiffComment, 'id' | 'n'>;

const EMPTY: readonly IDiffComment[] = [];

/**
 * Comments on diff lines waiting in the composer of a conversation tab, outside React: the changes panel
 * adds them, the composer shows them as chips and drops them after a successful send.
 */
export const createDiffCommentsStore = () => {
  const byTab = new Map<string, readonly IDiffComment[]>();
  const listeners = new Set<() => void>();
  let ids = 0;
  const get = (tab: string): readonly IDiffComment[] => byTab.get(tab) ?? EMPTY;
  const set = (tab: string, list: readonly IDiffComment[]) => {
    if (list.length === 0) byTab.delete(tab);
    else byTab.set(tab, list);
    for (const listener of listeners) listener();
  };
  return {
    get,
    add: (tab: string, comment: TNewDiffComment): IDiffComment => {
      const list = get(tab);
      const added: IDiffComment = { ...comment, id: `d${++ids}`, n: (list.at(-1)?.n ?? 0) + 1 };
      set(tab, [...list, added]);
      return added;
    },
    remove: (tab: string, id: string): void =>
      set(
        tab,
        get(tab).filter((comment) => comment.id !== id),
      ),
    clear: (tab: string): void => set(tab, EMPTY),
    subscribe: (listener: () => void): (() => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

const store = createDiffCommentsStore();

export const readDiffComments = (tab: string): readonly IDiffComment[] => store.get(tab);
export const addDiffComment = (tab: string, comment: TNewDiffComment): IDiffComment =>
  store.add(tab, comment);
export const removeDiffComment = (tab: string, id: string): void => store.remove(tab, id);
export const clearDiffComments = (tab: string): void => store.clear(tab);

export const useDiffComments = (tab: string): readonly IDiffComment[] =>
  useSyncExternalStore(
    store.subscribe,
    () => store.get(tab),
    () => EMPTY,
  );
