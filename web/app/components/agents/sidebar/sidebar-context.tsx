import { createContext, useContext } from 'react';
import { useStore, type StoreApi } from 'zustand';
import type { TProviderStatus } from '@/components/agents/provider-icon';
import type { IMachineFeatures } from '@/types/server/hl-machines';
import type { Conversation } from '@/lib/hl-conversations';
import type { TNewConversationKind } from '@/lib/hl-new-conversation';
import type { IOrganizer, IOrganizerProject, TConversationRef } from '@/lib/hl-organizer';
import type { IRowModel, ISidebarMachine } from '@/lib/hl-sidebar-model';

/** What changes with every turn of an agent: the rows read it one key at a time. */
export interface ISidebarLive {
  selectedKey: string | null;
  pendingKeys: ReadonlySet<string>;
  statuses: Readonly<Record<string, TProviderStatus>>;
}

/** What every row, folder and menu of the left panel needs from the shell. */
export interface ISidebarContext extends ISidebarLive {
  busy: boolean;
  features: IMachineFeatures | null;
  /** null while it loads or while the default machine does not answer: organizer actions are off. */
  organizer: IOrganizer | null;
  onSelect: (item: Conversation) => void;
  /** Opens a row of the model; an away machine or a deleted chat answers with a toast. */
  onOpenRow: (row: IRowModel) => void;
  onCreate: (kind: TNewConversationKind, projectId?: string) => void;
  /** The machines of the person's fleet; null until they are known or without a fleet. */
  machines: readonly ISidebarMachine[] | null;
  /** «Continue on machine»: a new conversation there, started from the handoff note of this one's agent. */
  onContinueOn: (conversation: Conversation, machine: ISidebarMachine) => void;
  /** «New project…»: asks for a name, then creates it with `refs` inside. */
  onNewProject: (refs?: TConversationRef[]) => void;
  onRenameProject: (project: IOrganizerProject) => void;
  onDeleteProject: (project: IOrganizerProject) => void;
}

/** The shell's callbacks and the organizer without the live part, so a status change does not reach every row. */
export type TSidebarActions = Omit<ISidebarContext, keyof ISidebarLive>;

export const SidebarContext = createContext<ISidebarContext | null>(null);
export const SidebarActionsContext = createContext<TSidebarActions | null>(null);
export const SidebarLiveContext = createContext<StoreApi<ISidebarLive> | null>(null);

export const useSidebarContext = (): ISidebarContext => {
  const context = useContext(SidebarContext);
  if (!context) throw new Error('useSidebarContext outside SidebarContext');
  return context;
};

export const useSidebarActions = (): TSidebarActions => {
  const actions = useContext(SidebarActionsContext);
  if (!actions) throw new Error('useSidebarActions outside SidebarActionsContext');
  return actions;
};

/** One conversation's slice of the live state: a turn of one agent re-renders its own row, not the whole list. */
export const useSidebarRowState = (ref: string) => {
  const live = useContext(SidebarLiveContext);
  if (!live) throw new Error('useSidebarRowState outside SidebarLiveContext');
  const status = useStore(live, (s) => s.statuses[ref] ?? 'idle');
  const active = useStore(live, (s) => s.selectedKey === ref);
  const pending = useStore(live, (s) => s.pendingKeys.has(ref));
  return { status, active, pending };
};
