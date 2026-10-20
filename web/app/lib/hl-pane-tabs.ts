/** Tabs of the right panel of a conversation, as in Codex desktop: pure transitions and their storage. */

export const PANE_TAB_KINDS = [
  'overview',
  'tools',
  'browser',
  'terminal',
  'files',
  'disk',
  'changes',
  'artifacts',
  'gitlab',
  'subagents',
  'economics',
  'plan',
  'peers',
  'design',
  'side-chat',
  'gallery',
  'task',
  'vault',
] as const;
export type TPaneTabKind = (typeof PANE_TAB_KINDS)[number];

/** One of these per conversation: the target of a browser is where it goes, not which browser it is. */
const SINGLE_KINDS: readonly TPaneTabKind[] = ['overview', 'tools', 'browser', 'terminal', 'vault'];

/** The islands of the conversation: the first tab of every panel, it cannot be closed. */
export const OVERVIEW_TAB_ID = 'overview' as const;

export interface IPaneTab {
  id: string;
  kind: TPaneTabKind;
  /** A file path, a document, a port or an address; null opens the kind's own list. */
  target: string | null;
  title: string | null;
  /** Opened by itself behind the tab the person was on, and not looked at yet. */
  unread?: boolean;
  /** Bumped by every request to open a target here: asking again for the same page shows it again. */
  asked?: number;
}

export interface IPaneState {
  tabs: IPaneTab[];
  activeId: string;
  /** What auto-open has already acted on (`hl-pane-auto`); absent until the conversation was first looked at. */
  seen?: readonly string[];
}

export interface IPaneTabRequest {
  kind: TPaneTabKind;
  target?: string | null;
  title?: string | null;
}

const TARGET_MAX = 2048;

/** Window event and shell → frame message that open a tab; a shell with a machine's conversation on screen forwards it. */
export const OPEN_PANE_TAB_EVENT = 'hl:open-pane-tab' as const;

export const isPaneTabKind = (value: unknown): value is TPaneTabKind =>
  typeof value === 'string' && (PANE_TAB_KINDS as readonly string[]).includes(value);

const isOptionalText = (value: unknown): boolean =>
  value === undefined || value === null || (typeof value === 'string' && value.length <= TARGET_MAX);

export const isPaneTabRequest = (value: unknown): value is IPaneTabRequest => {
  if (typeof value !== 'object' || value === null) return false;
  const { kind, target, title } = value as Record<string, unknown>;
  return isPaneTabKind(kind) && isOptionalText(target) && isOptionalText(title);
};

export const paneTabId = (kind: TPaneTabKind, target: string | null): string =>
  SINGLE_KINDS.includes(kind) || target === null ? kind : `${kind}:${target}`;

const overviewTab = (): IPaneTab => ({ id: OVERVIEW_TAB_ID, kind: 'overview', target: null, title: null });

export const newPaneState = (): IPaneState => ({ tabs: [overviewTab()], activeId: OVERVIEW_TAB_ID });

export const activePaneTab = (state: IPaneState): IPaneTab =>
  state.tabs.find((tab) => tab.id === state.activeId) ?? state.tabs[0];

const read = (tab: IPaneTab): IPaneTab => {
  if (!tab.unread) return tab;
  const { unread: _unread, ...rest } = tab;
  return rest;
};

export const activateTab = (state: IPaneState, id: string): IPaneState => {
  const target = state.tabs.find((tab) => tab.id === id);
  if (!target || (state.activeId === id && !target.unread)) return state;
  return { ...state, tabs: state.tabs.map((tab) => (tab.id === id ? read(tab) : tab)), activeId: id };
};

interface IOpenOptions {
  title?: string | null;
  /** The new tab the tool was chosen on: it becomes that tool in place, as in Codex. */
  replace?: string;
  /** Opened by itself while the person is busy in another tab: it waits behind, marked unread. */
  background?: boolean;
}

/** Opens a tab of this kind and target or brings the one already open forward. */
export const openTab = (
  state: IPaneState,
  kind: TPaneTabKind,
  target: string | null = null,
  { title = null, replace, background = false }: IOpenOptions = {},
): IPaneState => {
  const id = paneTabId(kind, target);
  const replaced =
    replace !== undefined &&
    replace !== id &&
    state.tabs.some((tab) => tab.id === replace && tab.kind === 'tools')
      ? replace
      : null;
  const existing = state.tabs.find((tab) => tab.id === id);
  const behind = background && state.activeId !== id;
  const activeId = behind ? state.activeId : id;
  const shown = (tab: IPaneTab): IPaneTab => (behind ? { ...tab, unread: true } : read(tab));
  if (existing) {
    const updated = shown({
      ...existing,
      target: target ?? existing.target,
      title: title ?? existing.title,
      asked: target ? (existing.asked ?? 0) + 1 : existing.asked,
    });
    const tabs = state.tabs
      .filter((tab) => tab.id !== replaced)
      .map((tab) => (tab.id === id ? updated : tab));
    return { ...state, tabs, activeId };
  }
  const tab = shown({ id, kind, target, title, asked: target ? 1 : undefined });
  if (replaced)
    return {
      ...state,
      tabs: state.tabs.map((current) => (current.id === replaced ? tab : current)),
      activeId,
    };
  const at = state.tabs.findIndex((current) => current.id === state.activeId) + 1;
  return { ...state, tabs: [...state.tabs.slice(0, at), tab, ...state.tabs.slice(at)], activeId };
};

/** The right neighbour takes over a closed active tab, the left one at the end; the overview stays whatever is closed. */
export const closeTab = (state: IPaneState, id: string): IPaneState => {
  const index = state.tabs.findIndex((tab) => tab.id === id);
  if (index === -1 || id === OVERVIEW_TAB_ID) return state;
  const tabs = state.tabs.filter((tab) => tab.id !== id);
  if (tabs.length === 0) return { ...state, ...newPaneState() };
  if (state.activeId !== id) return { ...state, tabs };
  return { ...state, tabs, activeId: (tabs[index] ?? tabs[index - 1]).id };
};

/** A tab learns the name of what it shows (a document tells its title once loaded); nothing else about it changes. */
export const retitleTab = (state: IPaneState, id: string, title: string): IPaneState =>
  state.tabs.some((tab) => tab.id === id && tab.title !== title)
    ? { ...state, tabs: state.tabs.map((tab) => (tab.id === id ? { ...tab, title } : tab)) }
    : state;

export const stepTab = (state: IPaneState, offset: number): IPaneState => {
  const index = state.tabs.findIndex((tab) => tab.id === state.activeId);
  const count = state.tabs.length;
  return activateTab(state, state.tabs[(((index + offset) % count) + count) % count].id);
};

type TPaneKeyInput = Pick<KeyboardEvent, 'code' | 'metaKey' | 'ctrlKey' | 'altKey' | 'shiftKey'>;

/**
 * ⌥⌘S (Ctrl+Alt+S) and Ctrl+`, as in Codex; read from `code`, so they work on any layout. Nothing here takes Ctrl/⌘+W
 * or another key the browser keeps for its own tabs.
 */
export const paneShortcutFromEvent = (event: TPaneKeyInput): TPaneTabKind | null => {
  if (event.shiftKey) return null;
  if (event.code === 'KeyS' && event.altKey && (event.metaKey || event.ctrlKey)) return 'side-chat';
  if (event.code === 'Backquote' && event.ctrlKey && !event.metaKey && !event.altKey) return 'terminal';
  return null;
};

type TPaneStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

/** Conversations whose tabs are remembered; older ones start over with a new tab. */
export const PANE_HISTORY_LIMIT = 40;
const INDEX_KEY = 'hl:pane-tabs';

export const paneStorageKey = (key: string): string => `${INDEX_KEY}:${key}`;

const readTab = (value: unknown): IPaneTab | null => {
  if (typeof value !== 'object' || value === null) return null;
  const { kind, target = null, title = null } = value as Record<string, unknown>;
  if (!isPaneTabKind(kind) || !isOptionalText(target) || !isOptionalText(title)) return null;
  const at = target as string | null;
  return { id: paneTabId(kind, at), kind, target: at, title: title as string | null };
};

const readJson = (storage: TPaneStorage | null, key: string): unknown => {
  try {
    const raw = storage?.getItem(key);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
};

/** Ids auto-open keeps per conversation; the oldest go first. */
export const PANE_SEEN_LIMIT = 300;

const readSeen = (value: unknown): string[] | undefined =>
  Array.isArray(value)
    ? value
        .filter((item): item is string => typeof item === 'string' && item.length <= TARGET_MAX)
        .slice(-PANE_SEEN_LIMIT)
    : undefined;

export const loadPaneTabs = (storage: TPaneStorage | null, key: string): IPaneState => {
  const saved = readJson(storage, paneStorageKey(key));
  if (typeof saved !== 'object' || saved === null || !Array.isArray((saved as { tabs?: unknown }).tabs))
    return newPaneState();
  const tabs: IPaneTab[] = [overviewTab()];
  for (const tab of (saved as { tabs: unknown[] }).tabs.map(readTab)) {
    if (tab && !tabs.some((known) => known.id === tab.id)) tabs.push(tab);
  }
  const { activeId, seen } = saved as { activeId?: unknown; seen?: unknown };
  const known = readSeen(seen);
  // A panel saved before the overview existed held only its untouched new tab: it starts on the overview now.
  const untouched = tabs.length === 2 && tabs[1].kind === 'tools' && activeId === 'tools';
  if (untouched) return { ...newPaneState(), ...(known ? { seen: known } : {}) };
  return {
    tabs,
    activeId: tabs.some((tab) => tab.id === activeId) ? (activeId as string) : tabs[0].id,
    ...(known ? { seen: known } : {}),
  };
};

export const savePaneTabs = (storage: TPaneStorage | null, key: string, state: IPaneState): void => {
  if (!storage) return;
  try {
    const index = readJson(storage, INDEX_KEY);
    const known = Array.isArray(index)
      ? index.filter((item): item is string => typeof item === 'string' && item !== key)
      : [];
    const kept = [...known, key];
    for (const old of kept.splice(0, Math.max(0, kept.length - PANE_HISTORY_LIMIT)))
      storage.removeItem(paneStorageKey(old));
    storage.setItem(
      paneStorageKey(key),
      JSON.stringify({
        tabs: state.tabs.map(({ kind, target, title }) => ({ kind, target, title })),
        activeId: state.activeId,
        ...(state.seen ? { seen: state.seen.slice(-PANE_SEEN_LIMIT) } : {}),
      }),
    );
    storage.setItem(INDEX_KEY, JSON.stringify(kept));
  } catch {}
};
