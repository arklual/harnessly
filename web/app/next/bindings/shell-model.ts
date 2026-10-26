// The shell's data on the stores: the sidebar's rows and machine card, the header's chat and its actions. The shell
// components take plain props; this is where the stores become them. Every action is a command that settles with
// words (outcome.ts), so a row is busy until the machine answers and says why when it does not.
import { useCallback, useMemo } from 'react';
import { shortModelName } from '@/next/features/composer/agents';
import { useBeta } from '@/next/stores/beta';
import { appBasePath } from '@/platform/config';
import {
  createFavGroup,
  deleteConversation,
  stopTerminal,
  forkConversation,
  deleteFavGroup,
  moveConversation,
  moveFavGroup,
  pinConversation,
  placeFavorite,
  renameConversation,
  retitleConversation,
  renameFavGroup,
} from '@/next/commands';
import { failureText, settled } from '@/next/bindings/outcome';
import type { IMachineNotice } from '@/next/features/notifications/from-machines';
import { hub } from '@/next/core/realtime/hub';
import { chatStatusOf, statusNoteOf } from '@/next/features/sidebar/status';
import {
  MACHINE_HEALTH,
  MACHINE_TONE,
  type IChatActions,
  type IMachineAlert,
  type IMachineCard,
  type IMachineChoice,
  type ISidebarChat,
  type ISidebarFavorites,
  type ISidebarProject,
  type TMachineHealth,
  type TMachineTone,
} from '@/next/features/sidebar/types';
import { locale, plural, t } from '@/next/i18n';
import { goTo, routePath, ROUTE } from '@/next/routes/routes';
import { requestContinue, requestUi, UI_REQUEST } from '@/next/stores/ui-requests';
import type {
  Conversation,
  HealthIssue,
  HealthMetric,
  Machine,
  MachineId,
  MachineLoad,
  Project,
} from '@/next/protocol/v2';
import { useConnection } from '@/next/stores/connection';
import { conversationsStore, useConversationList } from '@/next/stores/conversations';
import { useActiveMachine, useMachines, type MachineView } from '@/next/stores/machines';
import { useFavorites } from '@/next/stores/favorites';
import { useProjects } from '@/next/stores/projects';

const GB = 1024 ** 3;
const AGENT_NAME = { claude: 'Claude Code', codex: 'Codex' } as const;

/** Gigabytes the way the card reads them: one decimal under ten, whole above. */
const gb = (bytes: number): string => {
  const n = bytes / GB;
  return (n < 10 ? Math.round(n * 10) / 10 : Math.round(n)).toLocaleString(locale());
};

/** A machine without a name of its own is called by the first label of its host, not the whole domain. */
const machineName = (m: { name: string; host: string }): string =>
  m.name && m.name !== m.host ? m.name : m.host.split('.')[0] || m.host;

const baseName = (path: string): string => path.split('/').filter(Boolean).pop() ?? path;

/** The projects the list shows: the machine's projects, and a folder a conversation names a project by that the
 *  machine has not listed, named by the folder. */
export const projectsOf = (
  convs: readonly Conversation[],
  listed: readonly Project[] = [],
): ISidebarProject[] => {
  const byId = new Map<string, ISidebarProject>();
  for (const p of listed) byId.set(p.id, { id: p.id, name: p.name });
  for (const c of convs) {
    if (c.projectId && !byId.has(c.projectId))
      byId.set(c.projectId, { id: c.projectId, name: baseName(c.cwd) });
  }
  return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name));
};

/** The tone of a level of the machine's verdict, for its dot and its lines. */
const HEALTH_OF_LEVEL = {
  ok: MACHINE_HEALTH.ok,
  warn: MACHINE_HEALTH.warn,
  critical: MACHINE_HEALTH.critical,
} as const;
const TONE_OF_LEVEL = { warn: MACHINE_TONE.warn, critical: MACHINE_TONE.critical } as const;

/** The reason under a line of the card, per metric and level. */
const REASON_KEY = {
  disk: { warn: 'next.health.reason.diskWarn', critical: 'next.health.reason.diskCritical' },
  mem: { warn: 'next.health.reason.memWarn', critical: 'next.health.reason.memCritical' },
  cpu: { warn: 'next.health.reason.cpuWarn', critical: 'next.health.reason.cpuWarn' },
} as const;
const METRIC_ICON = { disk: 'hard-drive', mem: 'memory-stick', cpu: 'cpu' } as const;

const issueOf = (machine: Machine, metric: HealthMetric): HealthIssue | undefined =>
  machine.health?.issues.find((i) => i.metric === metric);

/** A machine that answers is as healthy as it says; one the page cannot hear is bad, one not asked yet is warn. */
const healthOf = (machine: Machine, linkDown: boolean): TMachineHealth => {
  if (machine.reach === 'unreachable' || linkDown) return MACHINE_HEALTH.bad;
  if (machine.reach === 'unknown') return MACHINE_HEALTH.warn;
  return HEALTH_OF_LEVEL[machine.health?.level ?? 'ok'];
};

const lineOf = (machine: Machine, metric: HealthMetric): { tone: TMachineTone; reason: string | null } => {
  const issue = issueOf(machine, metric);
  if (!issue) return { tone: MACHINE_TONE.normal, reason: null };
  return { tone: TONE_OF_LEVEL[issue.level], reason: t(REASON_KEY[metric][issue.level]) };
};

/** The banner's line of a critical issue: with the figure when the load is here, without it otherwise. */
const alertText = (issue: HealthIssue, load: MachineLoad | undefined): string => {
  if (issue.metric === 'disk')
    return load && load.diskTotal > 0
      ? t('next.health.alert.diskFree', { free: gb(load.diskFree) })
      : t('next.health.alert.disk');
  if (issue.metric === 'mem')
    return load && load.memTotal > 0
      ? t('next.health.alert.memOf', { used: gb(load.memUsed), total: gb(load.memTotal) })
      : t('next.health.alert.mem');
  return t('next.health.alert.cpu');
};

/** The first critical issue of a machine that answers, as the banner and the notification say it. */
export const alertOf = (machine: Machine, linkDown: boolean): IMachineAlert | null => {
  if (linkDown || machine.reach !== 'online' || machine.health?.level !== 'critical') return null;
  const critical = machine.health.issues.filter((i) => i.level === 'critical');
  const first = critical[0];
  if (!first) return null;
  return {
    key: `${machine.id}:${critical.map((i) => i.metric).join(',')}`,
    icon: METRIC_ICON[first.metric],
    text: alertText(first, machine.load),
  };
};

export const cardOf = (view: MachineView, linkDown: boolean): IMachineCard => {
  const { machine } = view;
  const health = healthOf(machine, linkDown);
  const load = machine.load;
  const away = health === MACHINE_HEALTH.bad;
  const mem = load ? load.memUsed / load.memTotal : 0;
  const diskUsed = load && load.diskTotal > 0 ? 1 - load.diskFree / load.diskTotal : 0;
  const apps = machine.agents
    .filter((a) => a.status !== 'missing' && a.version)
    .map((a) => `${AGENT_NAME[a.provider]} ${a.version}`)
    .join(' · ');
  const notes = [
    apps,
    load && load.busyAgents > 0 ? plural(load.busyAgents, 'next.shellData.busyAgents') : '',
  ];
  return {
    id: machine.id,
    name: machineName(machine),
    health,
    issue: away ? { title: t('next.shellData.awayTitle'), text: t('next.shellData.awayText') } : null,
    meters:
      load && !away
        ? [
            {
              id: 'cpu',
              icon: METRIC_ICON.cpu,
              label: t('next.shellData.cpu'),
              value: `${Math.round(load.cpu * 100)}%`,
              ratio: load.cpu,
              ...lineOf(machine, 'cpu'),
            },
            {
              id: 'mem',
              icon: METRIC_ICON.mem,
              label: t('next.shellData.memory'),
              value: t('next.shellData.memoryOf', { used: gb(load.memUsed), total: gb(load.memTotal) }),
              ratio: mem,
              ...lineOf(machine, 'mem'),
            },
            ...(load.diskTotal > 0
              ? [
                  {
                    id: 'disk',
                    icon: METRIC_ICON.disk,
                    label: t('next.shellData.disk'),
                    value: t('next.shellData.diskFree', { free: gb(load.diskFree) }),
                    ratio: diskUsed,
                    ...lineOf(machine, 'disk'),
                  },
                ]
              : []),
          ]
        : null,
    notes: notes.filter(Boolean),
    alert: alertOf(machine, linkDown),
  };
};

/** The word under a machine that cannot be chosen; a machine that is fine says nothing (its host is no help to anyone). */
const noteOf = (view: MachineView): string => {
  if (view.runtime === 'outdated') return t('next.shellData.outdated');
  if (view.machine.reach === 'unreachable') return t('next.shellData.away');
  return '';
};

export const choiceOf = (view: MachineView): IMachineChoice => {
  const off = view.machine.reach === 'unreachable' || view.runtime === 'outdated';
  return {
    id: view.machine.id,
    name: machineName(view.machine),
    health: off ? MACHINE_HEALTH.off : HEALTH_OF_LEVEL[view.machine.health?.level ?? 'ok'],
    note: noteOf(view),
    off,
  };
};

const copyText = async (text: string) => {
  if (!navigator.clipboard) throw new Error(t('next.shellData.noClipboard'));
  await navigator.clipboard.writeText(text);
};

const machineOfChat = (id: string): MachineId => {
  const record = [...conversationsStore.values()].find((c) => c.id === id);
  if (!record) throw new Error(t('next.refuse.not-found'));
  return record.machineId;
};

/** The agent's terminal runs, or is starting: a chat stopped, gone or never started has none, nor one whose terminal
 *  was stopped and not started again. */
export const terminalRuns = (c: Pick<Conversation, 'state' | 'terminalStop'>): boolean =>
  c.terminalStop === undefined &&
  c.state.k !== 'stopped' &&
  c.state.k !== 'gone' &&
  c.state.k !== 'launch-failed';

const HOUR_MS = 3_600_000;

/** What the feed says under a chat whose terminal was ended and is not back: null while one runs. */
export const terminalStopText = (c: Pick<Conversation, 'state' | 'terminalStop'>): string | null => {
  const stop = c.terminalStop;
  if (!stop || c.state.k !== 'stopped') return null;
  if (stop.by !== 'idle') return t('next.terminalStop.person');
  return t('next.terminalStop.idle', { n: Math.max(1, Math.round((stop.idleMs ?? 0) / HOUR_MS)) });
};

/** The chat's address in the new interface, for «Скопировать ссылку». */
const chatUrl = (id: string) =>
  `${window.location.origin}${appBasePath()}${routePath({ kind: ROUTE.chat, id })}`;

export interface IShellModel {
  active: MachineId | null;
  chats: readonly ISidebarChat[] | null;
  projects: readonly ISidebarProject[] | null;
  favorites: ISidebarFavorites;
  machine: IMachineCard | null;
  otherMachines: readonly IMachineChoice[];
  /** The machines about to fail their work, the open one first, for the notification centre. */
  machineNotices: readonly IMachineNotice[];
  actions: IChatActions;
  conversations: readonly Conversation[] | null;
  /** The machine and the connection of the active machine, for banners. */
  linkDown: boolean;
  pickMachine: (id: string) => Promise<void>;
}

export function useShellModel(): IShellModel {
  const { retitle } = useBeta();
  const active = useActiveMachine();
  const machines = useMachines();
  const convs = useConversationList(active);
  const connection = useConnection(active);
  const linkDown = connection?.k === 'stale' || connection?.k === 'offline';

  const features = machines?.find((v) => v.machine.id === active)?.features;
  const canStop = features?.includes('terminal-stop') ?? false;
  const canFork = features?.includes('fork') ?? false;
  const canHandoff = features?.includes('handoff') ?? false;
  const chats = useMemo<readonly ISidebarChat[] | null>(
    () =>
      convs &&
      convs.map((c) => {
        const terminal = terminalRuns(c);
        const status = chatStatusOf(c, !linkDown);
        const waitsFor = statusNoteOf(c, status);
        return {
          id: c.id,
          title: c.title || t('next.shellData.untitled'),
          status,
          ...(waitsFor ? { waitsFor } : {}),
          ...(c.parentId ? { parentId: c.parentId } : {}),
          pinned: c.pinned,
          projectId: c.projectId,
          createdAt: c.createdAt,
          lastPromptAt: c.lastPromptAt,
          lastActivityAt: c.lastActivityAt,
          terminal,
          canStopTerminal: canStop && terminal && c.state.k !== 'starting',
          ...(c.launch
            ? {
                launch: {
                  id: c.launch.id,
                  prompt: c.launch.prompt,
                  model: c.model ? shortModelName(c.model.id) : c.provider,
                },
              }
            : {}),
          canFork: canFork && c.lastTurn !== null,
          canMerge: canFork && c.origin?.k === 'fork',
          canContinue: canHandoff && c.lastTurn !== null,
        };
      }),
    [convs, linkDown, canStop, canFork, canHandoff],
  );
  const favorites = useFavorites(active);
  const listedProjects = useProjects(active);
  const projects = useMemo(() => convs && projectsOf(convs, listedProjects), [convs, listedProjects]);

  const activeView = machines?.find((v) => v.machine.id === active) ?? null;
  const machine = useMemo(() => (activeView ? cardOf(activeView, linkDown) : null), [activeView, linkDown]);
  const otherMachines = useMemo(
    () => (machines ?? []).filter((v) => v.machine.id !== active).map(choiceOf),
    [machines, active],
  );
  const machineNotices = useMemo(
    () =>
      [...(activeView ? [activeView] : []), ...(machines ?? []).filter((v) => v !== activeView)].flatMap(
        ({ machine: m }): IMachineNotice[] => {
          const alert = alertOf(m, m.id === active && linkDown);
          return alert && m.health ? [{ id: m.id, name: machineName(m), alert, since: m.health.since }] : [];
        },
      ),
    [machines, activeView, active, linkDown],
  );

  const actions = useMemo<IChatActions>(() => {
    const chat = (id: string) => id as Conversation['id'];
    // The groups of the favorites belong to the machine that is open; without one there is nothing to group.
    const groups: IChatActions = active
      ? {
          onPlaceFavorite: (id, groupId, beforeId) =>
            settled(
              placeFavorite(machineOfChat(id), chat(id), groupId, beforeId === null ? null : chat(beforeId)),
            ),
          onCreateGroup: async (name) => {
            const st = await createFavGroup(active, name).done;
            if (st.k === 'failed') throw new Error(failureText(st.failure));
            return st.k === 'done' ? (st.result.id ?? null) : null;
          },
          onRenameGroup: (id, name) => settled(renameFavGroup(active, id, name)),
          onDeleteGroup: (id) => settled(deleteFavGroup(active, id)),
          onMoveGroup: (id, beforeId) => settled(moveFavGroup(active, id, beforeId)),
        }
      : {};
    return {
      ...groups,
      onRename: (id, title) =>
        settled(renameConversation(machineOfChat(id), id as Conversation['id'], title)),
      onRetitle: retitle
        ? (id) => settled(retitleConversation(machineOfChat(id), id as Conversation['id']))
        : undefined,
      onPin: (id, pinned) => settled(pinConversation(machineOfChat(id), id as Conversation['id'], pinned)),
      onMove: (id, projectId) =>
        settled(moveConversation(machineOfChat(id), id as Conversation['id'], projectId)),
      onDelete: (id) => settled(deleteConversation(machineOfChat(id), id as Conversation['id'])),
      // The person confirmed in the menu, which says that work going on stops too.
      onStopTerminal: (id) => settled(stopTerminal(machineOfChat(id), id as Conversation['id'], true)),
      onCopyLink: (id) => copyText(chatUrl(id)),
      onFork: async (id) => {
        const handle = forkConversation(machineOfChat(id), chat(id));
        await settled(handle);
        const conv = await handle.conversation;
        if (conv) goTo({ kind: ROUTE.chat, id: conv });
      },
      onMerge: () => requestUi(UI_REQUEST.merge),
      onContinue: (id) => {
        goTo({ kind: ROUTE.chat, id });
        requestContinue(id);
      },
    };
  }, [active, retitle]);

  const pickMachine = useCallback((id: string) => {
    hub.setActive(id);
    return Promise.resolve();
  }, []);

  return {
    active,
    chats,
    projects,
    favorites,
    machine,
    otherMachines,
    machineNotices,
    actions,
    conversations: convs,
    linkDown,
    pickMachine,
  };
}
