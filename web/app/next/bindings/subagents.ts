// The subagents of a conversation as the stores have them, in the order they were started: the entries of the feed
// that stand for an agent. An agent has its launch entry in the turn that started it and, when its task notification
// woke the agent between turns, a copy that opens the next turn: one agent, once, by its id.
import { useCallback, useRef, useSyncExternalStore } from 'react';
import type { ConvId, Entry } from '@/next/protocol/v2';
import { entryStore } from '@/next/stores/conversation';

export type TSubagent = Extract<Entry, { t: 'subagent' }>;

const NONE: readonly TSubagent[] = [];

const collect = (conv: ConvId): TSubagent[] => {
  const prefix = `${conv} `;
  const byAgent = new Map<string, TSubagent>();
  for (const key of entryStore.keys()) {
    if (!key.startsWith(prefix)) continue;
    const entry = entryStore.get(key);
    if (entry?.t !== 'subagent') continue;
    const have = byAgent.get(entry.agentId);
    if (!have || entry.at < have.at) byAgent.set(entry.agentId, entry);
  }
  return [...byAgent.values()].sort((a, b) => a.at - b.at);
};

const sameAgents = (a: readonly TSubagent[], b: readonly TSubagent[]) =>
  a.length === b.length && a.every((x, i) => x === b[i]);

/** The conversation's subagents; the same array until one of them changes. */
export const useSubagents = (conv: ConvId | null): readonly TSubagent[] => {
  const last = useRef<{ conv: ConvId | null; version: number; list: readonly TSubagent[] }>({
    conv: null,
    version: -1,
    list: NONE,
  });
  const subscribe = useCallback(
    (notify: () => void) =>
      entryStore.subscribeAll((keys) => {
        if (conv === null) return;
        for (const k of keys) {
          if (k.startsWith(`${conv} `)) {
            notify();
            return;
          }
        }
      }),
    [conv],
  );
  const snapshot = () => {
    if (conv === null) return NONE;
    const seen = last.current;
    const version = entryStore.version();
    if (seen.conv === conv && seen.version === version) return seen.list;
    const next = collect(conv);
    const list = seen.conv === conv && sameAgents(seen.list, next) ? seen.list : next;
    last.current = { conv, version, list };
    return list;
  };
  return useSyncExternalStore(subscribe, snapshot, snapshot);
};
