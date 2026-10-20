// The runtime's workspaces (GET /api/workspace) and which one is open.
import { create } from 'zustand';
import { workspaces as api } from '@/runtime/endpoints';
import type { IWorkspace, IWorkspaceGroup } from '@/types/terminal';

export interface IWorkspaceInitialData {
  workspaces: IWorkspace[];
  groups?: IWorkspaceGroup[];
  activeWorkspaceId?: string | null;
}

interface IWorkspaceState {
  workspaces: IWorkspace[];
  groups: IWorkspaceGroup[];
  activeWorkspaceId: string | null;
  loaded: boolean;
  hydrate: (data: IWorkspaceInitialData | null | undefined) => void;
  fetchWorkspaces: () => Promise<void>;
  switchWorkspace: (id: string) => void;
}

const useWorkspaceStore = create<IWorkspaceState>((set, get) => ({
  workspaces: [],
  groups: [],
  activeWorkspaceId: null,
  loaded: false,
  hydrate: (data) => {
    if (!data) return;
    set({
      workspaces: data.workspaces ?? [],
      groups: data.groups ?? [],
      activeWorkspaceId: data.activeWorkspaceId ?? data.workspaces?.[0]?.id ?? null,
      loaded: true,
    });
  },
  fetchWorkspaces: async () => {
    const list = await api.list();
    get().hydrate({
      workspaces: list.workspaces,
      groups: list.groups,
      activeWorkspaceId: get().activeWorkspaceId ?? list.activeWorkspaceId ?? null,
    });
  },
  switchWorkspace: (id) => {
    if (get().activeWorkspaceId === id) return;
    set({ activeWorkspaceId: id });
    void api.setActive(id).catch(() => {});
  },
}));

export default useWorkspaceStore;
