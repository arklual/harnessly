import { useCallback, useState } from 'react';
import type { Conversation } from '@/lib/hl-conversations';
import type { ISidebarHistory } from '@/components/agents/sidebar/sidebar-header';

const MAX_ENTRIES = 50;

interface IHistoryState {
  entries: Conversation[];
  index: number;
  /** Key being opened by ← or →: its arrival must not become a new entry. */
  travelling: string | null;
}

const arrive = (current: IHistoryState, selected: Conversation): IHistoryState => {
  if (current.travelling === selected.key) return { ...current, travelling: null };
  if (current.entries[current.index]?.key === selected.key) return current;
  const neighbour = [current.index - 1, current.index + 1].find(
    (index) => current.entries[index]?.key === selected.key,
  );
  if (neighbour !== undefined) return { ...current, index: neighbour };
  const entries = [...current.entries.slice(0, current.index + 1), selected].slice(-MAX_ENTRIES);
  return { entries, index: entries.length - 1, travelling: null };
};

/** ← and → of the panel header: conversations opened in this page, kept in memory only. The browser's Back and Forward move along it too. */
const useSelectionHistory = (
  selected: Conversation | null,
  select: (item: Conversation) => void,
): ISidebarHistory => {
  const [state, setState] = useState<IHistoryState>({ entries: [], index: -1, travelling: null });
  const selectedKey = selected && selected.id !== 'new' && selected.key !== 'shared' ? selected.key : null;

  // Only a new key is a new entry; a title update of the same conversation is not.
  const [seenKey, setSeenKey] = useState<string | null>(null);
  if (selectedKey !== seenKey) {
    setSeenKey(selectedKey);
    if (selectedKey && selected) setState(arrive(state, selected));
  }

  const travel = useCallback(
    (delta: -1 | 1) => {
      const target = state.entries[state.index + delta];
      if (!target) return;
      setState((current) => ({ ...current, index: current.index + delta, travelling: target.key }));
      select(target);
    },
    [select, state.entries, state.index],
  );

  return {
    canBack: state.index > 0,
    canForward: state.index >= 0 && state.index < state.entries.length - 1,
    onBack: () => travel(-1),
    onForward: () => travel(1),
  };
};

export default useSelectionHistory;
