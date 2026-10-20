import {
  ArrowDown,
  ArrowUp,
  FolderInput,
  MonitorUp,
  FolderMinus,
  FolderPlus,
  Link2,
  Palette,
  PencilLine,
  Pin,
  PinOff,
  Plus,
  Skull,
  Trash2,
  X,
} from 'lucide-react';
import { toast } from 'sonner';
import { TW_MENU_KIND, type TTwMenuEntry } from '@/components/agents/ui';
import ProviderIcon from '@/components/agents/provider-icon';
import ProjectTile from '@/components/agents/sidebar/project-tile';
import { organizerActions, PROJECT_COLOR_LABELS } from '@/components/agents/sidebar/organizer-actions';
import type { TSidebarActions } from '@/components/agents/sidebar/sidebar-context';
import type { TProviderStatus } from '@/components/agents/provider-icon';
import { newConversationOptions, type TNewConversationKind } from '@/lib/hl-new-conversation';
import { ORGANIZER_LIMITS, PROJECT_COLORS, type TConversationRef } from '@/lib/hl-organizer';
import { ROW_STATE, type IProjectModel, type IRowModel } from '@/lib/hl-sidebar-model';
import type { IMachineFeatures } from '@/types/server/hl-machines';
import type { Conversation } from '@/lib/hl-conversations';
import { controlSession } from '@/lib/hl-session-control';
import { copyConversationLink } from '@/lib/hl-conversation-link';
import { t } from '@/lib/i18n';

const OFFLINE_REASON = t('sidebar.menus.offlineReason');

const KILLABLE_AGENTS: Record<string, string> = { claude: 'Claude', codex: 'Codex' };

const killAgent = async (conversation: Conversation, name: string) => {
  try {
    const { killed } = await controlSession<{ killed: number }>(
      'kill',
      conversation.id,
      conversation.machineId,
    );
    if (killed)
      toast.success(t('sidebar.menus.stopped', { name }), {
        description: t('sidebar.menus.stoppedDescription'),
      });
    else toast(t('sidebar.menus.notRunning', { name }));
  } catch (error) {
    toast.error(t('sidebar.menus.stopFailed', { name }), {
      description: error instanceof Error ? error.message : undefined,
    });
  }
};

/** «Kill Claude» while the conversation's agent runs: it and everything it started stop, the conversation stays. */
const killItem = (
  conversation: Conversation | null | undefined,
  status: TProviderStatus,
): TTwMenuEntry | null => {
  if (conversation?.kind !== 'agent') return null;
  const name = KILLABLE_AGENTS[conversation.provider];
  if (!name || status === 'idle') return null;
  return {
    kind: TW_MENU_KIND.action,
    id: 'kill',
    label: `Kill ${name}`,
    icon: <Skull />,
    danger: true,
    onSelect: () => void killAgent(conversation, name),
  };
};

/** The agents whose session can write a handoff note (a fork of the session answers; see the side chat). */
const HANDOFF_AGENTS: readonly string[] = ['claude'];

/** «Continue on machine ▸»: the other machines of the fleet, for a conversation whose agent can hand the work over. */
const continueItem = (
  conversation: Conversation | null | undefined,
  context: TSidebarActions,
): TTwMenuEntry | null => {
  if (conversation?.kind !== 'agent' || !HANDOFF_AGENTS.includes(conversation.provider)) return null;
  const others = (context.machines ?? []).filter((machine) => machine.id !== conversation.machineId);
  if (!conversation.machineId || others.length === 0) return null;
  return {
    kind: TW_MENU_KIND.submenu,
    id: 'continue-on',
    label: t('sidebar.menus.continueOn'),
    icon: <MonitorUp />,
    items: others.map<TTwMenuEntry>((machine) => ({
      kind: TW_MENU_KIND.action,
      id: `continue-on-${machine.id}`,
      label: machine.name,
      disabled: !machine.available,
      disabledReason: machine.available ? undefined : t('sidebar.menus.machineNotResponding'),
      onSelect: () => context.onContinueOn(conversation, machine),
    })),
  };
};

/** «Move to project ▸» and friends for one conversation; the same list serves ⋯, right click and the palette. */
export const conversationMenuItems = (
  ref: TConversationRef,
  context: TSidebarActions,
  status: TProviderStatus,
  row?: IRowModel,
  conversation?: Conversation | null,
): TTwMenuEntry[] => {
  const { organizer } = context;
  const offline = organizer === null;
  const pinned = organizer?.pins.includes(ref) ?? false;
  const projectId = organizer?.assignments[ref] ?? null;
  const gone = row?.state === ROW_STATE.chatMissing || row?.state === ROW_STATE.machineRemoved;
  const pinsFull = !pinned && (organizer?.pins.length ?? 0) >= ORGANIZER_LIMITS.pins;
  const items: TTwMenuEntry[] = [];

  if (gone) {
    items.push({
      kind: TW_MENU_KIND.action,
      id: 'forget',
      label: t('sidebar.menus.forget'),
      icon: <X />,
      onSelect: () => void organizerActions.forget([ref]),
    });
    return items;
  }
  items.push({
    kind: TW_MENU_KIND.action,
    id: 'pin',
    label: pinned ? t('sidebar.menus.unpin') : t('sidebar.menus.pin'),
    icon: pinned ? <PinOff /> : <Pin />,
    disabled: offline || pinsFull,
    disabledReason: offline
      ? OFFLINE_REASON
      : pinsFull
        ? t('sidebar.menus.pinsFull', { count: ORGANIZER_LIMITS.pins })
        : undefined,
    onSelect: () => void (pinned ? organizerActions.unpin(ref) : organizerActions.pin(ref)),
  });
  if (pinned && organizer) {
    const index = organizer.pins.indexOf(ref);
    if (index > 0)
      items.push({
        kind: TW_MENU_KIND.action,
        id: 'pin-up',
        label: t('sidebar.menus.pinUp'),
        icon: <ArrowUp />,
        onSelect: () => void organizerActions.movePin(organizer.pins, ref, -1),
      });
    if (index < organizer.pins.length - 1)
      items.push({
        kind: TW_MENU_KIND.action,
        id: 'pin-down',
        label: t('sidebar.menus.pinDown'),
        icon: <ArrowDown />,
        onSelect: () => void organizerActions.movePin(organizer.pins, ref, 1),
      });
  }
  items.push({
    kind: TW_MENU_KIND.submenu,
    id: 'move',
    label: t('sidebar.menus.move'),
    icon: <FolderInput />,
    disabled: offline,
    disabledReason: offline ? OFFLINE_REASON : undefined,
    items: [
      ...(organizer?.projects ?? []).map<TTwMenuEntry>((project) => ({
        kind: TW_MENU_KIND.radio,
        id: `move-${project.id}`,
        label: project.name,
        icon: <ProjectTile project={project} size="sm" />,
        checked: project.id === projectId,
        onSelect: () => {
          if (project.id !== projectId) void organizerActions.assign([ref], project.id);
        },
      })),
      ...(organizer?.projects.length ? [{ kind: TW_MENU_KIND.separator, id: 'move-sep' } as const] : []),
      {
        kind: TW_MENU_KIND.action,
        id: 'move-new',
        label: t('sidebar.menus.newProject'),
        icon: <FolderPlus />,
        onSelect: () => context.onNewProject([ref]),
      },
    ],
  });
  if (projectId) {
    items.push({
      kind: TW_MENU_KIND.action,
      id: 'unassign',
      label: t('sidebar.menus.unassign'),
      icon: <FolderMinus />,
      onSelect: () => void organizerActions.assign([ref], null),
    });
  }
  items.push({
    kind: TW_MENU_KIND.action,
    id: 'copy-link',
    label: t('sidebar.menus.copyLink'),
    icon: <Link2 />,
    onSelect: () => void copyConversationLink(ref),
  });
  const onward = continueItem(conversation, context);
  if (onward) items.push(onward);
  const kill = killItem(conversation, status);
  if (kill) items.push({ kind: TW_MENU_KIND.separator, id: 'kill-sep' }, kill);
  return items;
};

const newConversationItems = (
  features: IMachineFeatures | null,
  onCreate: (kind: TNewConversationKind) => void,
  idPrefix: string,
): TTwMenuEntry[] =>
  newConversationOptions(features).map((option) => ({
    kind: TW_MENU_KIND.action,
    id: `${idPrefix}-${option.kind}`,
    label: option.title,
    hint: option.hint,
    icon: <ProviderIcon provider={option.kind} size="sm" />,
    onSelect: () => onCreate(option.kind),
  }));

export const newConversationMenuItems = (context: TSidebarActions, projectId?: string): TTwMenuEntry[] =>
  newConversationItems(
    context.features,
    (kind) => context.onCreate(kind, projectId),
    projectId ? `new-${projectId}` : 'new',
  );

export const projectMenuItems = (model: IProjectModel, context: TSidebarActions): TTwMenuEntry[] => {
  const { project, staleRefs } = model;
  const projects = context.organizer?.projects ?? [];
  const index = projects.findIndex((p) => p.id === project.id);
  const items: TTwMenuEntry[] = [
    {
      kind: TW_MENU_KIND.submenu,
      id: 'new',
      label: t('sidebar.menus.newInProject'),
      icon: <Plus />,
      items: newConversationMenuItems(context, project.id),
    },
    { kind: TW_MENU_KIND.separator, id: 'sep-1' },
    {
      kind: TW_MENU_KIND.action,
      id: 'rename',
      label: t('sidebar.menus.rename'),
      icon: <PencilLine />,
      onSelect: () => context.onRenameProject(project),
    },
    {
      kind: TW_MENU_KIND.submenu,
      id: 'color',
      label: t('sidebar.menus.color'),
      icon: <Palette />,
      items: PROJECT_COLORS.map((color) => ({
        kind: TW_MENU_KIND.radio,
        id: `color-${color}`,
        label: PROJECT_COLOR_LABELS[color],
        icon: <ProjectTile project={{ ...project, color }} size="sm" />,
        checked: project.color === color,
        onSelect: () => void organizerActions.recolorProject(project.id, color),
      })),
    },
  ];
  if (index > 0)
    items.push({
      kind: TW_MENU_KIND.action,
      id: 'up',
      label: t('sidebar.menus.up'),
      icon: <ArrowUp />,
      onSelect: () => void organizerActions.moveProject(projects, project.id, -1),
    });
  if (index >= 0 && index < projects.length - 1)
    items.push({
      kind: TW_MENU_KIND.action,
      id: 'down',
      label: t('sidebar.menus.down'),
      icon: <ArrowDown />,
      onSelect: () => void organizerActions.moveProject(projects, project.id, 1),
    });
  if (staleRefs.length) {
    items.push({
      kind: TW_MENU_KIND.action,
      id: 'forget',
      label: t('sidebar.menus.forgetClosed', { count: staleRefs.length }),
      icon: <X />,
      onSelect: () => void organizerActions.forget(staleRefs),
    });
  }
  items.push({ kind: TW_MENU_KIND.separator, id: 'sep-2' });
  items.push({
    kind: TW_MENU_KIND.action,
    id: 'delete',
    label: t('sidebar.menus.deleteProject'),
    icon: <Trash2 />,
    danger: true,
    onSelect: () => context.onDeleteProject(project),
  });
  return items;
};
