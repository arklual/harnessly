// What the sidebar draws, in its own words: a store (or the bridge to the current interface) turns its data into these.
// The names follow the v2 model (§1 of the spec): a conversation's id, its project, its pin, its last activity.

/** The one icon a row shows for its agent; `none` keeps the row quiet. */
export const CHAT_STATUS = {
  none: 'none',
  thinking: 'thinking',
  /** The prompt is free, but background work (subagents, commands, monitors, a wakeup) goes on: the chat is not done. */
  background: 'background',
  question: 'question',
  perm: 'perm',
  plan: 'plan',
  done: 'done',
  failed: 'failed',
  stopped: 'stopped',
  waitingMachine: 'waiting-machine',
  /** The agent hit its plan's usage limit; the chat waits for the reset and goes on by itself or by the person's word. */
  limited: 'limited',
} as const;
export type TChatStatus = (typeof CHAT_STATUS)[keyof typeof CHAT_STATUS];

export interface ISidebarChat {
  id: string;
  title: string;
  status: TChatStatus;
  /** For `background`: what the agent waits for, in words («Ждёт: 2 сабагента, команда npm test»); for `limited`: «Ждёт сброса лимита до 18:00»; else absent. */
  waitsFor?: string;
  pinned: boolean;
  projectId: string | null;
  /** The chat whose agent delegated this one: it is drawn under that chat. Absent for a chat the person started. */
  parentId?: string;
  /** Unix ms the chat was created. */
  createdAt: number;
  /**
   * Unix ms of the person's last message (the creation time of a chat with none): it fixes the chat's place (newest
   * first) and its day group. The agent's replies, its work and its status do not move it.
   */
  lastPromptAt: number;
  /** Unix ms of the last activity; it does not move the chat in the lists. */
  lastActivityAt: number;
  /** The agent's terminal runs (or is starting); a chat without one is drawn muted. */
  terminal: boolean;
  /** «Завершить терминал» can end it now: it runs and the machine can stop terminals. */
  canStopTerminal: boolean;
  /** «Ответвить»: the chat has a turn to branch off after and the machine can fork. */
  canFork?: boolean;
  /** «Влить обратно»: the chat is a fork of another one, which its result can go back to. */
  canMerge?: boolean;
  /** «Продолжить в другой модели»: the chat has a turn to carry over and the machine can hand off. */
  canContinue?: boolean;
  /** Set on a chat started with others by one prompt on several models: they stand together under the prompt. */
  launch?: ISidebarLaunch;
}

/** One of the chats a prompt started on several models: the launch it belongs to and the model it runs. */
export interface ISidebarLaunch {
  id: string;
  /** The prompt, as the group's name. */
  prompt: string;
  /** The model's name, as the chat's name inside the group. */
  model: string;
}

export interface ISidebarProject {
  id: string;
  name: string;
}

/** A group inside the favorites, the ids of its chats in the order shown. */
export interface ISidebarFavGroup {
  id: string;
  name: string;
  chats: readonly string[];
}

/**
 * How the machine keeps the favorites laid out: the groups in their order, then the chats of none. Which chats are
 * favorites is their own `pinned`; a pinned chat this does not name comes first among the loose.
 */
export interface ISidebarFavorites {
  groups: readonly ISidebarFavGroup[];
  loose: readonly string[];
}

export const NO_FAVORITES: ISidebarFavorites = { groups: [], loose: [] };

/** warn and critical are the machine's own verdict on its disk, memory and CPU; bad is a machine that does not answer. */
export const MACHINE_HEALTH = {
  ok: 'ok',
  warn: 'warn',
  critical: 'critical',
  bad: 'bad',
  off: 'off',
} as const;
export type TMachineHealth = (typeof MACHINE_HEALTH)[keyof typeof MACHINE_HEALTH];

export const MACHINE_TONE = { normal: 'normal', warn: 'warn', critical: 'critical' } as const;
export type TMachineTone = (typeof MACHINE_TONE)[keyof typeof MACHINE_TONE];

/** One line of the machine card: an icon, a label, a value and a bar of `ratio` (0…1). */
export interface IMachineMeter {
  id: string;
  icon: string;
  label: string;
  value: string;
  ratio: number;
  tone: TMachineTone;
  /** Why the line is in its tone, in a few words; null when it is fine. */
  reason: string | null;
}

/** The machine is about to fail its work: the one line of the sidebar's banner. The key changes when it worsens. */
export interface IMachineAlert {
  key: string;
  icon: string;
  text: string;
}

export interface IMachineCard {
  id: string;
  name: string;
  health: TMachineHealth;
  /** What is wrong, when something is. */
  issue: { title: string; text: string } | null;
  /** The load; null until the machine sent it. */
  meters: readonly IMachineMeter[] | null;
  /** The two short notes under the meters (open apps, running agents). */
  notes: readonly string[];
  /** The banner of a critical machine; null while it is not critical. */
  alert: IMachineAlert | null;
}

/** Another machine in the card's list. */
export interface IMachineChoice {
  id: string;
  name: string;
  health: TMachineHealth;
  /** Why it cannot be chosen, in a word or two; empty for a machine that is fine. */
  note: string;
  /** It cannot be chosen now (unreachable). */
  off: boolean;
}

/** The person at the bottom of the sidebar: the name alone, a line under it that is cut short says nothing. */
export interface ISidebarPerson {
  name: string;
}

/** The footer's theme switch: dark is on now; the owner flips it. */
export interface ISidebarTheme {
  dark: boolean;
  onToggle: () => void;
}

/** One entry under «Ещё». */
export interface IMoreItem {
  id: string;
  icon: string;
  label: string;
  current: boolean;
  onSelect: () => void;
}

export const SIDEBAR_VIEW = { chat: 'chat', designly: 'designly', settings: 'settings' } as const;
export type TSidebarView = (typeof SIDEBAR_VIEW)[keyof typeof SIDEBAR_VIEW];

/**
 * What the sidebar can do. Every change is a promise: the row stays busy until it settles, and the list moves only
 * when the new data comes in. An action the data source cannot do yet is left out and its menu item is not shown.
 */
export interface IChatActions {
  onRename?: (id: string, title: string) => Promise<void>;
  /** The chat is named anew by a model (a beta function: absent while it is off). */
  onRetitle?: (id: string) => Promise<void>;
  onPin?: (id: string, pinned: boolean) => Promise<void>;
  onMove?: (id: string, projectId: string | null) => Promise<void>;
  onDelete?: (id: string) => Promise<void>;
  /** A chat into the favorites (pinned when it is not): in a group or, with null, ungrouped; before a chat or last. */
  onPlaceFavorite?: (id: string, groupId: string | null, beforeId: string | null) => Promise<void>;
  /** A chat dropped on the composer of the open chat: its next message points at it; absent where no chat is open. */
  onAttachChat?: (id: string) => void;
  /** A new group; resolves with its id. */
  onCreateGroup?: (name: string) => Promise<string | null>;
  onRenameGroup?: (id: string, name: string) => Promise<void>;
  /** The group goes, its chats stay in the favorites. */
  onDeleteGroup?: (id: string) => Promise<void>;
  /** The group goes before another one, or last with null. */
  onMoveGroup?: (id: string, beforeId: string | null) => Promise<void>;
  /** A new chat in a project: the start page opens with the project chosen. */
  onNewChatIn?: (projectId: string) => void;
  onCopyLink?: (id: string) => Promise<void>;
  /** The chat in a tab of its own behind the one on screen; absent where there is no strip of tabs. */
  onOpenInNewTab?: (id: string) => void;
  /** The agent's terminal of a conversation (the phone shows it in the menu). */
  onTerminal?: (id: string) => void;
  /** Ends the chat's terminal and everything in it; the chat stays and the next message starts the agent again. */
  onStopTerminal?: (id: string) => Promise<void>;
  /** A new chat that branches off after the chat's last finished turn; the new chat opens. */
  onFork?: (id: string) => Promise<void>;
  /** Opens the open chat's «Влить обратно» sheet (a fork's result back into the chat it came from). */
  onMerge?: (id: string) => void;
  /** Opens the chat (if it is not open) and its sheet to continue it in another model. */
  onContinue?: (id: string) => void;
  onNewProject?: () => void;
}
