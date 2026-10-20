import useWorkspaceStore from '@/hooks/use-workspace-store';
import { useLayoutStore } from '@/hooks/use-layout';
import { loadAndFocusAgent } from '@/lib/hl-agent-focus';
import { onViewMachineChange, viewMachineId } from '@/lib/app-path';
import { collectAllTabs } from '@/lib/layout-tree';
import type { ILayoutData, IWorkspace } from '@/types/terminal';

let sequence = 0;
let workspacesLoad: Promise<void> | null = null;
// A list on its way from the machine the shell no longer draws is not the list of the one it draws now.
// The layout in the store belongs to the machine it was loaded from; a workspace id can repeat across machines.
let layoutMachine: string | undefined | null = undefined;
// The last layout of each machine the shell drew: going back to one shows its conversation at once, then refreshes it.
const kept = new Map<string, { workspaceId: string; layout: ILayoutData; workspaces: IWorkspace[] }>();
const keyOf = (machine: string | undefined) => machine ?? '';
let shownMachine = viewMachineId();
onViewMachineChange((next) => {
  const { layout, workspaceId } = useLayoutStore.getState();
  if (layoutMachine === shownMachine && layout && workspaceId) {
    kept.set(keyOf(shownMachine), {
      workspaceId,
      layout,
      workspaces: useWorkspaceStore.getState().workspaces,
    });
  }
  shownMachine = next;
  workspacesLoad = null;
  layoutMachine = null;
});

const focusKept = (machine: string | undefined, workspaceId: string, tabId: string): boolean => {
  const last = kept.get(keyOf(machine));
  if (
    !last ||
    last.workspaceId !== workspaceId ||
    !collectAllTabs(last.layout.root).some((tab) => tab.id === tabId)
  )
    return false;
  useWorkspaceStore.setState({ workspaces: last.workspaces, activeWorkspaceId: workspaceId });
  const layoutStore = useLayoutStore.getState();
  layoutStore.setWorkspaceId(workspaceId);
  layoutStore.setLayout(last.layout);
  if (!useLayoutStore.getState().focusTab(tabId)) return false;
  layoutMachine = machine;
  void useLayoutStore.getState().fetchLayout(workspaceId, true);
  return true;
};

const knowsWorkspace = (workspaceId: string) =>
  useWorkspaceStore.getState().workspaces.some((w) => w.id === workspaceId);

/**
 * The workspace list, asked once for everyone waiting on it. A page opened on a conversation asks for it
 * with the first requests instead of after the list of conversations, which put a round trip into every first open.
 */
export const loadWorkspaces = (): Promise<void> => {
  workspacesLoad ??= useWorkspaceStore
    .getState()
    .fetchWorkspaces()
    .finally(() => {
      workspacesLoad = null;
    });
  return workspacesLoad;
};

/** Focuses an agent tab, loading its workspace layout first when needed. Later calls supersede earlier ones. */
export const openAgentTab = async (workspaceId: string, tabId: string): Promise<boolean> => {
  const current = ++sequence;
  const machine = viewMachineId();
  const loaded = useLayoutStore.getState();
  if (layoutMachine === machine && loaded.workspaceId === workspaceId && loaded.focusTab(tabId)) return true;
  if (layoutMachine !== machine && focusKept(machine, workspaceId, tabId)) return true;
  // A workspace the list already has needs no fresh list: only an unknown one is checked, since asking for the layout
  // of a workspace that does not exist creates it.
  if (!knowsWorkspace(workspaceId)) await loadWorkspaces();
  if (current !== sequence || !knowsWorkspace(workspaceId)) return false;
  const opened = await loadAndFocusAgent(
    {
      focusLoaded: (id, tab) => {
        const state = useLayoutStore.getState();
        return state.workspaceId === id && state.focusTab(tab);
      },
      setPending: (id, tab) => {
        useLayoutStore.setState({ pendingFocusTabId: tab });
        useLayoutStore.getState().setWorkspaceId(id);
        useWorkspaceStore.getState().switchWorkspace(id);
      },
      load: (id) => useLayoutStore.getState().fetchLayout(id, false),
      focus: (id) => useLayoutStore.getState().focusTab(id),
    },
    workspaceId,
    tabId,
    () => current === sequence,
  );
  if (opened && machine === viewMachineId()) layoutMachine = machine;
  return opened;
};
