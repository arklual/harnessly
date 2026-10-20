import type { TProviderStatus } from '@/components/agents/provider-icon';

/** What a collapsed project tells about its conversations: only states that ask for attention. */
export type TSummaryStatus = 'needs-input' | 'failed' | 'working' | 'ready';
import {
  groupConversations,
  parseConversationRef,
  type Conversation,
  type TConversationGroup,
} from '@/lib/hl-conversations';
import type { IOrganizer, IOrganizerProject, TConversationRef } from '@/lib/hl-organizer';

/**
 * What the left panel shows, as a pure function of the lists, the organizer and the device's view.
 * Rendered by the sidebar, the rail, the palette and the ⌘1…⌘9 handler, so every surface numbers rows the same way.
 */

export const ROW_STATE = {
  ok: 'ok',
  /** Its list has not answered yet (agents of the machine, or a chat outside the loaded pages). */
  loading: 'loading',
  machineUnavailable: 'machine-unavailable',
  machineRemoved: 'machine-removed',
  chatMissing: 'chat-missing',
  chatError: 'chat-error',
} as const;
export type TRowState = (typeof ROW_STATE)[keyof typeof ROW_STATE];

export const PROJECT_PAGE = 5;
export const HOTKEY_COUNT = 9;

export type TSidebarSection = 'pinned' | 'projects' | 'conversations' | 'docs';

export interface ISidebarView {
  collapsedProjects: string[];
  collapsedSections: TSidebarSection[];
}

export type TResolvedChat = Conversation | 'loading' | 'missing' | 'error';

export interface ISidebarMachine {
  id: string;
  name: string;
  available: boolean;
}

export interface IRowModel {
  ref: TConversationRef;
  /** Live entry, or the last one seen on this device while its machine is away; null when nothing is known. */
  conversation: Conversation | null;
  state: TRowState;
  /** 1…9 for ⌘N, null otherwise. */
  hotkey: number | null;
  /** Project of a pinned row, shown as a small tile. */
  project: IOrganizerProject | null;
  machineName: string | null;
}

export interface IProjectModel {
  project: IOrganizerProject;
  /** Every live row, newest first; the panel shows `shown` of them. */
  rows: IRowModel[];
  shown: number;
  collapsed: boolean;
  /** Members whose tab was closed on a machine that answered: hidden, cleaned up by hand. */
  staleRefs: TConversationRef[];
  /** The most urgent agent status inside, so a working agent in a folded project is not lost. */
  summary: { status: TSummaryStatus; count: number } | null;
}

export interface ISidebarModel {
  pinned: IRowModel[];
  pinnedStaleRefs: TConversationRef[];
  projects: IProjectModel[];
  /** Neither pinned nor in a project; the whole list while the organizer is unknown. */
  conversations: { group: TConversationGroup; items: Conversation[] }[];
  /** [0] is ⌘1. */
  hotkeys: IRowModel[];
  /** ref → project of every conversation, for chips in the palette and the header. */
  projectOf: (ref: string) => IOrganizerProject | null;
}

export interface IComposeSidebarInput {
  conversations: Conversation[];
  /** null while it loads or when the default machine does not answer: nothing is filtered then. */
  organizer: IOrganizer | null;
  view: ISidebarView;
  /** null while the machine list loads. */
  machines: ISidebarMachine[] | null;
  /** Machines that did not answer this round (fleet gateway `errors`). */
  unavailableMachineIds: readonly string[];
  /** Agent lists have answered at least once. */
  agentsLoaded: boolean;
  /** Chat id → result of the one-by-one lookup for chats outside the loaded pages. */
  resolvedChats: Readonly<Record<string, TResolvedChat>>;
  /** Last entries seen on this device, keyed by ref: titles of rows whose machine is away. */
  known: Readonly<Record<string, Conversation>>;
  /** Projects where «Show more» is open. */
  expandedLists: ReadonlySet<string>;
  statuses: Readonly<Record<string, TProviderStatus>>;
  now?: number;
}

const STATUS_RANK: Record<TSummaryStatus, number> = { 'needs-input': 4, failed: 3, working: 2, ready: 1 };
const isSummaryStatus = (status: TProviderStatus | undefined): status is TSummaryStatus =>
  !!status && status in STATUS_RANK;

type TRowDraft = Omit<IRowModel, 'hotkey'>;

const byRecency = (a: TRowDraft, b: TRowDraft) =>
  (b.conversation?.updatedAt ?? -1) - (a.conversation?.updatedAt ?? -1) || a.ref.localeCompare(b.ref);

export const composeSidebar = (input: IComposeSidebarInput): ISidebarModel => {
  const { organizer, view } = input;
  const byKey = new Map(input.conversations.map((item) => [item.key, item]));
  const unavailable = new Set(input.unavailableMachineIds);
  const machineById = new Map((input.machines ?? []).map((machine) => [machine.id, machine]));
  const projectById = new Map((organizer?.projects ?? []).map((project) => [project.id, project]));
  const projectOf = (ref: string) => projectById.get(organizer?.assignments[ref] ?? '') ?? null;

  /** null = stale: the list that should hold it answered without it. */
  const draft = (ref: TConversationRef): TRowDraft | null => {
    const live = byKey.get(ref);
    const project = projectOf(ref);
    if (live)
      return { ref, conversation: live, state: ROW_STATE.ok, project, machineName: live.machineName ?? null };
    const parsed = parseConversationRef(ref);
    if (!parsed) return null;
    const known = input.known[ref] ?? null;
    if (parsed.kind === 'chat') {
      const resolved = input.resolvedChats[parsed.id];
      if (resolved === 'missing')
        return { ref, conversation: known, state: ROW_STATE.chatMissing, project, machineName: null };
      if (resolved === 'error')
        return { ref, conversation: known, state: ROW_STATE.chatError, project, machineName: null };
      if (resolved && resolved !== 'loading')
        return { ref, conversation: resolved, state: ROW_STATE.ok, project, machineName: null };
      return { ref, conversation: known, state: ROW_STATE.loading, project, machineName: null };
    }
    const machineId = parsed.machineId;
    const machineName =
      (machineId && machineById.get(machineId)?.name) || known?.machineName || machineId || null;
    if (machineId && unavailable.has(machineId))
      return { ref, conversation: known, state: ROW_STATE.machineUnavailable, project, machineName };
    if (machineId && input.machines && !machineById.has(machineId))
      return { ref, conversation: known, state: ROW_STATE.machineRemoved, project, machineName };
    if (!input.agentsLoaded)
      return { ref, conversation: known, state: ROW_STATE.loading, project, machineName };
    return null;
  };

  const pinnedRefs = new Set(organizer?.pins ?? []);
  const pinned: TRowDraft[] = [];
  const pinnedStaleRefs: TConversationRef[] = [];
  for (const ref of organizer?.pins ?? []) {
    const row = draft(ref);
    if (row) pinned.push(row);
    else pinnedStaleRefs.push(ref);
  }

  const members = new Map<string, TConversationRef[]>();
  for (const [ref, projectId] of Object.entries(organizer?.assignments ?? {})) {
    if (!projectById.has(projectId)) continue;
    members.set(projectId, [...(members.get(projectId) ?? []), ref as TConversationRef]);
  }

  const collapsedProjects = new Set(view.collapsedProjects);
  const projectDrafts = (organizer?.projects ?? []).map((project) => {
    const refs = members.get(project.id) ?? [];
    const rows: TRowDraft[] = [];
    const staleRefs: TConversationRef[] = [];
    let summary: IProjectModel['summary'] = null;
    for (const ref of refs) {
      const row = draft(ref);
      if (!row) {
        if (!pinnedRefs.has(ref)) staleRefs.push(ref);
        continue;
      }
      const status = input.statuses[ref];
      if (isSummaryStatus(status)) {
        if (!summary || STATUS_RANK[status] > STATUS_RANK[summary.status]) summary = { status, count: 1 };
        else if (summary.status === status) summary.count += 1;
      }
      if (!pinnedRefs.has(ref)) rows.push(row);
    }
    rows.sort(byRecency);
    const shown = input.expandedLists.has(project.id) ? rows.length : Math.min(rows.length, PROJECT_PAGE);
    return { project, rows, shown, collapsed: collapsedProjects.has(project.id), staleRefs, summary };
  });

  // Numbers follow what is on screen, as in Codex: pins first, then the open projects' visible rows.
  const sections = new Set(view.collapsedSections);
  const numbered: TRowDraft[] = [];
  if (!sections.has('pinned')) numbered.push(...pinned);
  if (!sections.has('projects'))
    for (const p of projectDrafts) if (!p.collapsed) numbered.push(...p.rows.slice(0, p.shown));
  const hotkeyOf = new Map<string, number>();
  for (const row of numbered) {
    if (hotkeyOf.size >= HOTKEY_COUNT) break;
    if (row.state === ROW_STATE.chatMissing || row.state === ROW_STATE.machineRemoved) continue;
    hotkeyOf.set(row.ref, hotkeyOf.size + 1);
  }
  const finish = (row: TRowDraft): IRowModel => ({ ...row, hotkey: hotkeyOf.get(row.ref) ?? null });

  const pinnedRows = pinned.map(finish);
  const projects = projectDrafts.map((p) => ({ ...p, rows: p.rows.map(finish) }));
  const hotkeys = [...pinnedRows, ...projects.flatMap((p) => p.rows)]
    .filter((row) => row.hotkey !== null)
    .sort((a, b) => a.hotkey! - b.hotkey!);

  const loose = organizer
    ? input.conversations.filter(
        (item) =>
          !pinnedRefs.has(item.key as TConversationRef) &&
          !projectById.has(organizer.assignments[item.key] ?? ''),
      )
    : input.conversations;

  return {
    pinned: pinnedRows,
    pinnedStaleRefs,
    projects,
    conversations: groupConversations(loose, input.now),
    hotkeys,
    projectOf,
  };
};

/** Machine ids in the fleet gateway's `errors`, which the conversations hook joins as `<machineId>: <error>`. */
export const machineIdsFromFleetErrors = (errors: readonly string[]): string[] =>
  errors.map((line) => line.split(': ')[0]).filter(Boolean);
