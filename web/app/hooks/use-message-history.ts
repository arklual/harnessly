import { useCallback, useEffect } from 'react';
import { create } from 'zustand';
import { useShallow } from 'zustand/react/shallow';
import { appFetch } from '@/lib/app-path';
import { withEntry, withoutEntry, type IHistoryEntry } from '@/lib/hl-message-history';

interface IWorkspaceHistory {
  entries: IHistoryEntry[];
  isLoading: boolean;
  isError: boolean;
}

interface IHistoryState {
  byWorkspace: Record<string, IWorkspaceHistory>;
  fetchHistory: (workspaceId: string) => Promise<void>;
  addHistory: (workspaceId: string, message: string) => Promise<void>;
  deleteHistory: (workspaceId: string, id: string) => Promise<void>;
}

const EMPTY: IWorkspaceHistory = { entries: [], isLoading: false, isError: false };

const JSON_HEADERS = { 'Content-Type': 'application/json' };

export const useMessageHistoryStore = create<IHistoryState>((set, get) => {
  const patch = (workspaceId: string, change: Partial<IWorkspaceHistory>) =>
    set((s) => ({
      byWorkspace: {
        ...s.byWorkspace,
        [workspaceId]: { ...(s.byWorkspace[workspaceId] ?? EMPTY), ...change },
      },
    }));
  const entriesOf = (workspaceId: string) => (get().byWorkspace[workspaceId] ?? EMPTY).entries;

  return {
    byWorkspace: {},

    fetchHistory: async (workspaceId) => {
      patch(workspaceId, { isLoading: true, isError: false });
      try {
        const response = await appFetch(`/api/message-history?wsId=${encodeURIComponent(workspaceId)}`);
        if (!response.ok) throw new Error(String(response.status));
        const data = (await response.json()) as { entries?: IHistoryEntry[] };
        patch(workspaceId, { entries: data.entries ?? [], isLoading: false });
      } catch {
        patch(workspaceId, { isLoading: false, isError: true });
      }
    },

    // The list shows the message at once; the runtime's answer replaces the placeholder with the saved entry.
    addHistory: async (workspaceId, message) => {
      const text = message.trim();
      if (!text) return;
      const temp: IHistoryEntry = {
        id: `temp-${Date.now()}`,
        message: text,
        sentAt: new Date().toISOString(),
      };
      patch(workspaceId, { entries: withEntry(entriesOf(workspaceId), temp) });
      try {
        const response = await appFetch('/api/message-history', {
          method: 'POST',
          headers: JSON_HEADERS,
          body: JSON.stringify({ wsId: workspaceId, message: text }),
        });
        if (!response.ok) return;
        const data = (await response.json()) as { entry?: IHistoryEntry };
        const saved = data.entry;
        if (saved)
          patch(workspaceId, {
            entries: entriesOf(workspaceId).map((entry) => (entry.id === temp.id ? saved : entry)),
          });
      } catch {
        // The history is a convenience: a message that was sent stays sent.
      }
    },

    deleteHistory: async (workspaceId, id) => {
      const before = entriesOf(workspaceId);
      patch(workspaceId, { entries: withoutEntry(before, id) });
      try {
        const response = await appFetch('/api/message-history', {
          method: 'DELETE',
          headers: JSON_HEADERS,
          body: JSON.stringify({ wsId: workspaceId, id }),
        });
        if (!response.ok) throw new Error(String(response.status));
      } catch {
        patch(workspaceId, { entries: before });
      }
    },
  };
});

export interface IMessageHistory {
  entries: IHistoryEntry[];
  isLoading: boolean;
  isError: boolean;
  fetchHistory: () => Promise<void>;
  addHistory: (message: string) => Promise<void>;
  deleteHistory: (id: string) => Promise<void>;
}

/** The messages sent from this workspace, newest first. Without a workspace there is no history. */
export default function useMessageHistory(workspaceId: string | undefined): IMessageHistory {
  const { entries, isLoading, isError } = useMessageHistoryStore(
    useShallow((s) => (workspaceId ? (s.byWorkspace[workspaceId] ?? EMPTY) : EMPTY)),
  );
  const fetchInStore = useMessageHistoryStore((s) => s.fetchHistory);
  const addInStore = useMessageHistoryStore((s) => s.addHistory);
  const deleteInStore = useMessageHistoryStore((s) => s.deleteHistory);

  const fetchHistory = useCallback(async () => {
    if (workspaceId) await fetchInStore(workspaceId);
  }, [workspaceId, fetchInStore]);
  const addHistory = useCallback(
    async (message: string) => {
      if (workspaceId) await addInStore(workspaceId, message);
    },
    [workspaceId, addInStore],
  );
  const deleteHistory = useCallback(
    async (id: string) => {
      if (workspaceId) await deleteInStore(workspaceId, id);
    },
    [workspaceId, deleteInStore],
  );

  useEffect(() => {
    if (workspaceId) void fetchInStore(workspaceId);
  }, [workspaceId, fetchInStore]);

  return { entries, isLoading, isError, fetchHistory, addHistory, deleteHistory };
}
