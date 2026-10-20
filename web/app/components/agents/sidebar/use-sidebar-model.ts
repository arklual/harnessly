import { useEffect, useMemo } from 'react';
import useHlOrganizer from '@/hooks/use-hl-organizer';
import useSidebarView from '@/hooks/use-sidebar-view';
import useChatRefs from '@/hooks/use-chat-refs';
import { useHlMachines } from '@/hooks/use-hl-machines';
import { parseConversationRef, type Conversation } from '@/lib/hl-conversations';
import { composeSidebar, machineIdsFromFleetErrors, type ISidebarMachine } from '@/lib/hl-sidebar-model';
import useConversationStatuses from '@/components/agents/sidebar/use-conversation-statuses';
import useKnownConversations from '@/components/agents/sidebar/use-known-conversations';

/** Chats fetched one by one per page view at most; the rest wait for «Show more» or a scroll of the pages. */
const MAX_CHAT_LOOKUPS = 30;

interface IUseSidebarModelInput {
  conversations: Conversation[];
  fleetErrors: readonly string[];
  agentsLoaded: boolean;
  chatReady: boolean;
  /** The shell's own machine; the shell calls this hook above the RuntimeMachineContext it provides. */
  defaultMachineId?: string;
  /** The machine whose tabs the tab store holds now — the one whose conversation the shell draws itself. */
  statusMachineId?: string;
}

/** Organizer + lists + this device's folding → the model every left-panel surface renders. */
const useSidebarModel = ({
  conversations,
  fleetErrors,
  agentsLoaded,
  chatReady,
  defaultMachineId,
  statusMachineId,
}: IUseSidebarModelInput) => {
  const { organizer, error: organizerError, pendingKeys, apply, refresh } = useHlOrganizer();
  const view = useSidebarView((s) => s.view);
  const expandedLists = useSidebarView((s) => s.expandedLists);
  const hydrate = useSidebarView((s) => s.hydrate);
  useEffect(() => hydrate(), [hydrate]);

  const machinesData = useHlMachines().data?.machines;
  const machines = useMemo<ISidebarMachine[] | null>(
    () =>
      machinesData?.map((machine) => ({
        id: machine.id,
        name: machine.name,
        available: machine.available !== false,
      })) ?? null,
    [machinesData],
  );
  const unavailableMachineIds = useMemo(() => machineIdsFromFleetErrors(fleetErrors), [fleetErrors]);

  const organizerRefs = useMemo(
    () => (organizer ? [...new Set([...organizer.pins, ...Object.keys(organizer.assignments)])] : null),
    [organizer],
  );
  const known = useKnownConversations(conversations, organizerRefs);

  const missingChatIds = useMemo(() => {
    if (!organizer) return [];
    const loaded = new Set(conversations.map((item) => item.key));
    const collapsed = new Set(view.collapsedProjects);
    const onScreen = [
      ...organizer.pins,
      ...Object.entries(organizer.assignments)
        .filter(([, id]) => !collapsed.has(id))
        .map(([ref]) => ref),
    ];
    return onScreen
      .filter((ref) => !loaded.has(ref))
      .map((ref) => parseConversationRef(ref))
      .flatMap((ref) => (ref?.kind === 'chat' ? [ref.id] : []))
      .slice(0, MAX_CHAT_LOOKUPS);
  }, [organizer, conversations, view.collapsedProjects]);
  const resolvedChats = useChatRefs(missingChatIds, chatReady);

  const statuses = useConversationStatuses(conversations, statusMachineId ?? defaultMachineId);

  const model = useMemo(
    () =>
      composeSidebar({
        conversations,
        organizer,
        view,
        machines,
        unavailableMachineIds,
        agentsLoaded,
        resolvedChats,
        known,
        expandedLists,
        statuses,
      }),
    [
      conversations,
      organizer,
      view,
      machines,
      unavailableMachineIds,
      agentsLoaded,
      resolvedChats,
      known,
      expandedLists,
      statuses,
    ],
  );

  return {
    model,
    organizer,
    organizerError,
    pendingKeys,
    apply,
    refreshOrganizer: refresh,
    statuses,
    machines,
  };
};

export type TSidebarModelState = ReturnType<typeof useSidebarModel>;

export default useSidebarModel;
