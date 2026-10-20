import { useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from 'react';
import { createStore } from 'zustand';
import ProjectDialogs, { type TProjectDialog } from '@/components/agents/sidebar/project-dialogs';
import {
  SidebarActionsContext,
  SidebarContext,
  SidebarLiveContext,
  type ISidebarContext,
  type ISidebarLive,
  type TSidebarActions,
} from '@/components/agents/sidebar/sidebar-context';
import type { TSidebarModelState } from '@/components/agents/sidebar/use-sidebar-model';

/**
 * While ⌘ (Ctrl elsewhere) is held the rows swap their time for ⌘N, as in Codex: the numbers are there when
 * they are about to be used and do not crowd the titles otherwise. The flag lives on <html> for CSS.
 */
const MOD_ATTRIBUTE = 'data-tw-mod';

const useModifierHint = () => {
  useEffect(() => {
    const root = document.documentElement;
    const sync = (event: KeyboardEvent) =>
      root.toggleAttribute(MOD_ATTRIBUTE, event.metaKey || event.ctrlKey);
    const clear = () => root.removeAttribute(MOD_ATTRIBUTE);
    window.addEventListener('keydown', sync, true);
    window.addEventListener('keyup', sync, true);
    window.addEventListener('blur', clear);
    return () => {
      window.removeEventListener('keydown', sync, true);
      window.removeEventListener('keyup', sync, true);
      window.removeEventListener('blur', clear);
      clear();
    };
  }, []);
};

type TSidebarProviderProps = Pick<
  ISidebarContext,
  'selectedKey' | 'busy' | 'features' | 'onSelect' | 'onOpenRow' | 'onCreate' | 'onContinueOn'
> & {
  state: TSidebarModelState;
  children: ReactNode;
};

/** Shares the shell's callbacks and the organizer with the panel, the rail and their menus; owns the project dialogs. */
const SidebarProvider = ({
  state,
  selectedKey,
  busy,
  features,
  onSelect,
  onOpenRow,
  onCreate,
  onContinueOn,
  children,
}: TSidebarProviderProps) => {
  const [dialog, setDialog] = useState<TProjectDialog | null>(null);
  const { organizer, pendingKeys, statuses, machines } = state;
  const [live] = useState(() => createStore<ISidebarLive>(() => ({ selectedKey, pendingKeys, statuses })));
  useModifierHint();

  useLayoutEffect(
    () => live.setState({ selectedKey, pendingKeys, statuses }),
    [live, selectedKey, pendingKeys, statuses],
  );

  const actions = useMemo<TSidebarActions>(
    () => ({
      busy,
      features,
      organizer,
      onSelect,
      onOpenRow,
      onCreate,
      machines,
      onContinueOn,
      onNewProject: (refs = []) => setDialog({ kind: 'create', refs }),
      onRenameProject: (project) => setDialog({ kind: 'rename', project }),
      onDeleteProject: (project) => setDialog({ kind: 'delete', project }),
    }),
    [busy, features, organizer, onSelect, onOpenRow, onCreate, machines, onContinueOn],
  );
  const value = useMemo<ISidebarContext>(
    () => ({ ...actions, selectedKey, pendingKeys, statuses }),
    [actions, selectedKey, pendingKeys, statuses],
  );

  return (
    <SidebarActionsContext.Provider value={actions}>
      <SidebarLiveContext.Provider value={live}>
        <SidebarContext.Provider value={value}>
          {children}
          <ProjectDialogs
            dialog={dialog}
            projects={organizer?.projects ?? []}
            onClose={() => setDialog(null)}
          />
        </SidebarContext.Provider>
      </SidebarLiveContext.Provider>
    </SidebarActionsContext.Provider>
  );
};

export default SidebarProvider;
