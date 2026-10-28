// The shell's data on the stores: the sidebar's rows and machine card, the header's chat and its actions. The shell
// components take plain props; this is where the stores become them. Every action is a command that settles with
// words (outcome.ts), so a row is busy until the machine answers and says why when it does not.
import { useCallback, useMemo } from 'react';
import { shortModelName } from '@/next/features/composer/agents';
import { useBeta } from '@/next/stores/beta';
import { appBasePath } from '@/platform/config';
import {
  createFavGroup,
  createProject,
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
import { requestMoveVm, useMoveEnabledOn } from '@/next/stores/chat-move';
import type {
  Conversation,
  HealthIssue,
  HealthMetric,
  Machine,
  MachineId,
  MachineLoad,
  Project,
} from '@/next/protocol/v2';
import { connectionStore, useConnection } from '@/next/stores/connection';
import { byActivity, conversationsStore, useConversationList } from '@/next/stores/conversations';
import { sameItems, useAll } from '@/next/core/store';
import {
  homeMachineStore,
  useActiveMachine,
  useHomeMachine,
  useMachines,
  type MachineView,
} from '@/next/stores/machines';
import { movedKey, useCentral, type CentralView } from '@/next/stores/central';
import { useFavorites } from '@/next/stores/favorites';
import { useProjects } from '@/next/stores/projects';
import { editProfileSidebar, profileSidebarStore, useProfileSidebar } from '@/next/stores/profile-sidebar';
import {
  chatProjectOf,
  favGroupAdded,
  favGroupDeleted,
  favGroupMoved,
  favGroupRenamed,
  favPlaced,
  memberOn,
  newFavGroupId,
  profileProjectOf,
  withMember,
} from '@/next/features/sidebar/profile-layout';

const GB = 1024 ** 3;
const AGENT_NAME = { claude: 'Claude Code', codex: 'Codex' } as const;

/** Gigabytes the way the card reads them: one decimal under ten, whole above. */
const gb = (bytes: number): string => {
  const n = bytes / GB;
  return (n < 10 ? Math.round(n * 10) / 10 : Math.round(n)).toLocaleString(locale());
};

/** A machine without a name of its own is called by the first label of its host, not the whole domain. */
export const machineName = (m: { name: string; host: string }): string =>
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

/** A chat's status and what it waits for: the one reading of a conversation that the sidebar's row, the tab over the
 *  window and the header all draw, so no two of them say different things about the same chat. */
export const chatLookOf = (c: Conversation, online: boolean): Pick<ISidebarChat, 'status' | 'waitsFor'> => {
  const status = chatStatusOf(c, online);
  const waitsFor = statusNoteOf(c, status);
  return waitsFor ? { status, waitsFor } : { status };
};

/** What a tab over the window shows of its chat. */
export type ITabChat = Pick<ISidebarChat, 'id' | 'title' | 'status' | 'waitsFor'>;

/**
 * The chats of every machine for the tabs over the window: a tab may hold a chat of a machine other than the open one,
 * and it shows that chat's own status, read against that machine's own link. (The sidebar lists only the open
 * machine's chats; the tabs read them all, or a tab of another machine would lose its status on a switch.)
 */
/** Every machine's chats, newest first. */
const useEveryChat = () => useAll(conversationsStore, (s) => [...s.values()].sort(byActivity), sameItems);

/** The machines whose link is down, sorted (a stable list for the memos). */
const useDownMachines = () =>
  useAll(
    connectionStore,
    (s) =>
      [...s.keys()]
        .filter((m) => {
          const k = s.get(m)?.k;
          return k === 'stale' || k === 'offline';
        })
        .sort(),
    sameItems,
  );

/**
 * The one list of the person's chats on every machine: a chat continued on another machine gives its place
 * to the chat it went on as (it stays reachable by its address, where it says where it went).
 */
export const oneListOf = (
  convs: readonly Conversation[],
  here: MachineId,
  central: Pick<CentralView, 'on' | 'moved'>,
): Conversation[] =>
  convs.filter(
    (c) =>
      (c.machineId === here || central.on.has(c.machineId)) &&
      !central.moved.has(movedKey(c.machineId, c.id)),
  );

export function useTabChats(): ReadonlyMap<string, ITabChat> {
  const convs = useEveryChat();
  const down = useDownMachines();
  return useMemo(() => {
    const away = new Set(down);
    return new Map(
      convs.map((c): [string, ITabChat] => [
        c.id,
        {
          id: c.id,
          title: c.title || t('next.shellData.untitled'),
          ...chatLookOf(c, !away.has(c.machineId)),
        },
      ]),
    );
  }, [convs, down]);
}

/** Picking another machine while a chat of the open one is on screen: the screen goes to the picked machine's start
 *  page, so the sidebar, the machine card and the screen are of one machine (the chat stays in its tab). */
export const machineSwitchLeavesChat = (chatMachine: MachineId | null, picked: string): boolean =>
  chatMachine !== null && chatMachine !== picked;

/** The person's sidebar as it stands now; a change of it is refused while it has not come. */
const profileDoc = () => {
  const st = profileSidebarStore.get();
  if (st.k !== 'ready') throw new Error(t('next.profileSidebar.notReady'));
  return st.doc;
};

/**
 * The machine's project a project of the person stands for on `machine`: the one it has there, or a new one of
 * its name made there now and recorded in the person's sidebar (a project of the profile gets its folders on a machine
 * as its chats come there).
 */
export async function machineProjectOf(projectId: string, machine: MachineId): Promise<string> {
  const have = memberOn(profileDoc(), projectId, machine);
  if (have) return have;
  const project = profileDoc().projects.find((p) => p.id === projectId);
  if (!project) throw new Error(t('next.refuse.not-found'));
  const st = await createProject(machine, project.name).done;
  if (st.k === 'failed') throw new Error(failureText(st.failure));
  const made = st.k === 'done' ? st.result.id : undefined;
  if (!made) throw new Error(t('next.refuse.not-found'));
  await editProfileSidebar((doc) => withMember(doc, projectId, { machineId: machine, projectId: made }));
  return made;
}

/** A change of the favorites' layout of the person: refused with words when its group is gone. */
const editFavorites = (edit: Parameters<typeof editProfileSidebar>[0]) =>
  editProfileSidebar(edit).then(() => undefined);

export interface IShellModel {
  active: MachineId | null;
  chats: readonly ISidebarChat[] | null;
  projects: readonly ISidebarProject[] | null;
  favorites: ISidebarFavorites;
  machine: IMachineCard | null;
  otherMachines: readonly IMachineChoice[];
  /** Every machine a new chat may start on, the default one first. */
  startMachines: readonly IMachineChoice[];
  /** The projects and the favorites are the person's, kept by the cluster. */
  personSidebar: boolean;
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
  const home = useHomeMachine();
  const central = useCentral();
  const machines = useMachines();
  // with the cluster's copy on, the list is the person's, not a machine's: the chats of every machine the
  // copy is on for, the same list whichever machine the page goes through; a chat's machine is said in its menu only.
  // No machine is picked in the sidebar then: the card is the open chat's machine (the default one on the start page),
  // and a new chat's machine is picked in the model's popover. The projects and the favorites are the person's once
  // the cluster keeps them (below).
  const unified = central.enabled && home !== null;
  const here = unified ? home : active;
  // the projects and the favorites are the person's too when the cluster keeps them (profile-sidebar.ts):
  // the same folders and the same favorites whichever machine is picked, a chat in a folder whatever machine it is on.
  const profile = useProfileSidebar();
  const shared = unified && profile.k === 'ready' ? profile.doc : null;
  const sharedWaits = unified && profile.k === 'loading';
  const sharedOn = shared !== null;
  const projectIndex = useMemo(() => (shared ? profileProjectOf(shared) : null), [shared]);
  const own = useConversationList(here);
  const every = useEveryChat();
  const convs = useMemo(
    () => own && (unified && here ? oneListOf(every, here, central) : own),
    [own, unified, here, every, central],
  );
  const chatMachines = useMemo(() => [...new Set((convs ?? []).map((c) => c.machineId))].sort(), [convs]);
  const moveOn = useMoveEnabledOn(chatMachines);
  const connection = useConnection(here);
  const linkDown = connection?.k === 'stale' || connection?.k === 'offline';
  const down = useDownMachines();

  const chats = useMemo<readonly ISidebarChat[] | null>(() => {
    if (!convs) return null;
    const away = new Set(down);
    const views = new Map((machines ?? []).map((v) => [v.machine.id, v]));
    const nameOf = (m: MachineId): string => {
      const view = views.get(m);
      return view ? machineName(view.machine) : (central.names.get(m) ?? m);
    };
    const several = new Set([...views.keys(), ...convs.map((c) => c.machineId)]).size > 1;
    const projectOfChat = (c: Conversation): string | null => {
      if (projectIndex) return chatProjectOf(projectIndex, c.machineId, c.projectId);
      return c.machineId === here ? c.projectId : null;
    };
    return convs.map((c) => {
      const features = views.get(c.machineId)?.features ?? [];
      const canStop = features.includes('terminal-stop');
      const canFork = features.includes('fork');
      const canHandoff = features.includes('handoff');
      const terminal = terminalRuns(c);
      return {
        id: c.id,
        title: c.title || t('next.shellData.untitled'),
        ...chatLookOf(c, c.machineId === here ? !linkDown : !away.has(c.machineId)),
        // Where the chat lives is said in its menu only; the row is its title.
        ...(several ? { machine: nameOf(c.machineId) } : {}),
        ...(!projectIndex && c.machineId !== here ? { projectsElsewhere: true } : {}),
        ...(c.parentId ? { parentId: c.parentId } : {}),
        pinned: c.pinned,
        // The person's projects hold the chats of every machine; a machine's own project (the person's sidebar off)
        // is not one of another machine's, so such a chat stands outside the groups.
        projectId: projectOfChat(c),
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
        canMoveVm: moveOn.has(c.machineId) && c.lastTurn !== null,
      };
    });
  }, [convs, here, linkDown, down, machines, central.names, moveOn, projectIndex]);
  const machineFavorites = useFavorites(here);
  const favorites = shared ? shared.favorites : machineFavorites;
  const listedProjects = useProjects(here);
  const projects = useMemo(() => {
    if (!convs || sharedWaits) return null;
    if (shared)
      return shared.projects
        .map((p) => ({ id: p.id, name: p.name }))
        .sort((a, b) => a.name.localeCompare(b.name));
    return projectsOf(
      convs.filter((c) => c.machineId === here),
      listedProjects,
    );
  }, [convs, sharedWaits, shared, here, listedProjects]);

  // The card is the machine the page works with now: the open chat's (the shell makes it the active one), else here.
  const cardOn = active ?? here;
  const cardConnection = useConnection(cardOn);
  const cardDown = cardConnection?.k === 'stale' || cardConnection?.k === 'offline';
  const activeView = machines?.find((v) => v.machine.id === cardOn) ?? null;
  const machine = useMemo(() => (activeView ? cardOf(activeView, cardDown) : null), [activeView, cardDown]);
  const otherMachines = useMemo(
    () => (unified ? [] : (machines ?? []).filter((v) => v.machine.id !== here).map(choiceOf)),
    [unified, machines, here],
  );
  const startMachines = useMemo(
    () =>
      [
        ...(machines ?? []).filter((v) => v.machine.id === here),
        ...(machines ?? []).filter((v) => v.machine.id !== here),
      ].map(choiceOf),
    [machines, here],
  );
  const machineNotices = useMemo(
    () =>
      [...(activeView ? [activeView] : []), ...(machines ?? []).filter((v) => v !== activeView)].flatMap(
        ({ machine: m }): IMachineNotice[] => {
          const alert = alertOf(m, (m.id === cardOn && cardDown) || (m.id === here && linkDown));
          return alert && m.health ? [{ id: m.id, name: machineName(m), alert, since: m.health.since }] : [];
        },
      ),
    [machines, activeView, cardOn, cardDown, here, linkDown],
  );

  const actions = useMemo<IChatActions>(() => {
    const chat = (id: string) => id as Conversation['id'];
    // The groups of the favorites are the person's when the cluster keeps the sidebar: a chat is pinned on its own
    // machine and placed in the person's layout. Otherwise they belong to the machine that is open; without one there
    // is nothing to group.
    const personGroups: IChatActions = {
      onPlaceFavorite: async (id, groupId, beforeId) => {
        const record = conversationsStore.get(chat(id));
        if (!record) throw new Error(t('next.refuse.not-found'));
        if (!record.pinned) await settled(pinConversation(record.machineId, chat(id), true));
        await editFavorites((doc) =>
          favPlaced(doc, chat(id), groupId, beforeId === null ? null : chat(beforeId)),
        );
      },
      onCreateGroup: async (name) => {
        const id = newFavGroupId();
        await editFavorites((doc) => favGroupAdded(doc, id, name));
        return id;
      },
      onRenameGroup: (id, name) => editFavorites((doc) => favGroupRenamed(doc, id, name)),
      onDeleteGroup: (id) => editFavorites((doc) => favGroupDeleted(doc, id)),
      onMoveGroup: (id, beforeId) => editFavorites((doc) => favGroupMoved(doc, id, beforeId)),
    };
    const machineGroups: IChatActions = here
      ? {
          onPlaceFavorite: (id, groupId, beforeId) =>
            settled(
              placeFavorite(machineOfChat(id), chat(id), groupId, beforeId === null ? null : chat(beforeId)),
            ),
          onCreateGroup: async (name) => {
            const st = await createFavGroup(here, name).done;
            if (st.k === 'failed') throw new Error(failureText(st.failure));
            return st.k === 'done' ? (st.result.id ?? null) : null;
          },
          onRenameGroup: (id, name) => settled(renameFavGroup(here, id, name)),
          onDeleteGroup: (id) => settled(deleteFavGroup(here, id)),
          onMoveGroup: (id, beforeId) => settled(moveFavGroup(here, id, beforeId)),
        }
      : {};
    return {
      ...(sharedOn ? personGroups : machineGroups),
      onRename: (id, title) =>
        settled(renameConversation(machineOfChat(id), id as Conversation['id'], title)),
      onRetitle: retitle
        ? (id) => settled(retitleConversation(machineOfChat(id), id as Conversation['id']))
        : undefined,
      onPin: (id, pinned) => settled(pinConversation(machineOfChat(id), id as Conversation['id'], pinned)),
      onMove: async (id, projectId) => {
        const machine = machineOfChat(id);
        // A project of the person stands for a project of the chat's machine, made there when it has none yet.
        const target =
          sharedOn && projectId !== null ? await machineProjectOf(projectId, machine) : projectId;
        await settled(moveConversation(machine, chat(id), target));
      },
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
      onMoveVm: (id) => {
        goTo({ kind: ROUTE.chat, id });
        requestMoveVm(id);
      },
    };
  }, [here, retitle, sharedOn]);

  const pickMachine = useCallback((id: string) => {
    homeMachineStore.set(id);
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
    startMachines,
    personSidebar: sharedOn,
    machineNotices,
    actions,
    conversations: convs,
    linkDown,
    pickMachine,
  };
}
