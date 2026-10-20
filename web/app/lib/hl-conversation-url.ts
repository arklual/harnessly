import { APP_BASE_PATH } from '@/lib/app-path';
import {
  agentConversationKey,
  chatConversationKey,
  parseConversationRef,
  type Conversation,
  type TConversationRef,
} from '@/lib/hl-conversations';
import { LIBRARY_LIMITS, isLibraryId } from '@/lib/hl-design-library/types';
import { t } from '@/lib/i18n';

export const CENTRE_VIEW = { stats: 'stats', efficiency: 'efficiency', design: 'design' } as const;
export type TCentreView = (typeof CENTRE_VIEW)[keyof typeof CENTRE_VIEW];

const PARAM = {
  conversation: 'c',
  view: 'view',
  chat: 'chat',
  open: 'open',
  board: 'board',
  boardTitle: 'boardTitle',
  designProject: 'dproject',
  designBoard: 'dboard',
  designFull: 'dfull',
  designMachine: 'dmachine',
} as const;

/** The value of `dfull` that asks for the board alone in the window. */
const FULL_ON = '1';

/** Parameters the app itself owns in the address; everything else (`machine`, `runtime`, a debug flag) survives a move. */
const OWNED_PARAMS: readonly string[] = Object.values(PARAM);

/**
 * Where the design library view is: nothing (the projects), a project, or one of its boards (a path under `project/`,
 * query included). `full` shows that board alone in the window; it needs a board.
 */
export interface IDesignLocation {
  project: string | null;
  board: string | null;
  full: boolean;
  /** The machine the project lives on; absent, the project is looked up by id on every machine. */
  machine?: string;
}

export const DESIGN_HOME: IDesignLocation = { project: null, board: null, full: false };

export const sameDesignLocation = (a: IDesignLocation, b: IDesignLocation): boolean =>
  a.project === b.project &&
  a.board === b.board &&
  a.full === b.full &&
  (a.project === null || a.machine === b.machine);

// A machine id only travels as `?machine=` of a request; the gateway decides whether the person has it.
const MACHINE_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

const designLocationOf = (params: URLSearchParams): IDesignLocation => {
  const project = params.get(PARAM.designProject);
  if (!project || !isLibraryId(project)) return DESIGN_HOME;
  const asked = params.get(PARAM.designBoard);
  const board =
    asked && asked.length <= LIBRARY_LIMITS.boardPath && !/[\u0000-\u001f\\]/.test(asked) ? asked : null;
  const machine = params.get(PARAM.designMachine);
  return {
    project,
    board,
    full: board !== null && params.get(PARAM.designFull) === FULL_ON,
    ...(machine && MACHINE_ID_RE.test(machine) ? { machine } : {}),
  };
};

const OWN_CHAT_PATH_RE = /^\/c\/([a-zA-Z0-9-]+)$/;
const SHARED_CHAT_PATH_RE = /^\/s\/[a-zA-Z0-9-]+$/;
// An address is anyone's input, and its ids end up in request paths: no dots, so `..` never walks up a path.
const TAB_ID_RE = /^[A-Za-z0-9_-]{1,128}$/;
const ADDRESS_PART_RE = TAB_ID_RE;

const fromAddress = (ref: TConversationRef | null): TConversationRef | null => {
  if (!ref) return null;
  const parts = ref.kind === 'agent' ? [ref.id, ref.machineId ?? 'x'] : [ref.id];
  return parts.every((part) => ADDRESS_PART_RE.test(part)) ? ref : null;
};
const SHARED_KEY = 'shared';
const NEW_CHAT_ID = 'new';

export const conversationRefKey = (ref: TConversationRef): string =>
  ref.kind === 'chat' ? chatConversationKey(ref.id) : agentConversationKey(ref.id, ref.machineId);

/** Where a conversation lives in the address: the app root with its key, readable (`c=agent:runner-2:tab-1`). */
export const conversationSearch = (key: string): string =>
  `?${PARAM.conversation}=${encodeURIComponent(key).replace(/%3A/gi, ':')}`;

/** The path a link to the conversation has, base path included; `/agents/` keeps the page inside the service worker's scope. */
export const conversationHref = (key: string): string => `${APP_BASE_PATH}/${conversationSearch(key)}`;

export const conversationUrl = (origin: string, key: string): string => `${origin}${conversationHref(key)}`;

export interface IAppLocation {
  ref: TConversationRef | null;
  view: TCentreView | null;
  /** The place inside the design library view; present only for that view. */
  design?: IDesignLocation;
  /** The address still carries `open`, `chat` or a board request: it is rewritten to `c` once taken. */
  legacy: boolean;
  /** `?chat=/s/<id>`: a shared dialog in a frame; the address is not ours to rewrite. */
  shared: boolean;
}

const viewOf = (value: string | null): TCentreView | null =>
  Object.values(CENTRE_VIEW).find((view) => view === value) ?? null;

/** What an address asks the page to show: `c` first, then a view, then the older `chat` and `open` forms. */
export const parseAppLocation = (search: string): IAppLocation => {
  const params = new URLSearchParams(search);
  const chat = params.get(PARAM.chat);
  const open = params.get(PARAM.open);
  const legacy = chat !== null || open !== null || params.has(PARAM.board);
  const shared = chat !== null && SHARED_CHAT_PATH_RE.test(chat);
  const own = chat?.match(OWN_CHAT_PATH_RE);
  const parsed = fromAddress(parseConversationRef(params.get(PARAM.conversation) ?? ''));
  const conversation = parsed?.kind === 'chat' && parsed.id === NEW_CHAT_ID ? null : parsed;
  const view = viewOf(params.get(PARAM.view));
  if (conversation) return { ref: conversation, view: null, legacy, shared };
  if (view)
    return {
      ref: null,
      view,
      ...(view === CENTRE_VIEW.design ? { design: designLocationOf(params) } : {}),
      legacy,
      shared,
    };
  if (own) return { ref: { kind: 'chat', id: own[1] }, view: null, legacy, shared };
  if (open && TAB_ID_RE.test(open)) return { ref: { kind: 'agent', id: open }, view: null, legacy, shared };
  return { ref: null, view: null, legacy, shared };
};

/** An id from a notification, a Telegram link or `?open=`: a tab of some machine, the machine unnamed. */
export const agentRefOfTab = (tabId: string): TConversationRef | null =>
  TAB_ID_RE.test(tabId) ? { kind: 'agent', id: tabId } : null;

/** What the address should say for the state on screen; `provisional` marks a stop on the way (not found yet, not created yet). */
export interface IWantedLocation {
  key: string | null;
  view: TCentreView | null;
  provisional: boolean;
  /** The place inside the design library view; present only for that view. */
  design?: IDesignLocation;
}

/** null: the page shows something that has no address of its own (a shared dialog), leave the address alone. */
export const wantedLocation = (
  selected: Conversation | null,
  pending: TConversationRef | null,
  view: TCentreView | null,
  design: IDesignLocation = DESIGN_HOME,
): IWantedLocation | null => {
  if (selected?.key === SHARED_KEY) return null;
  if (selected) {
    const isNew = selected.kind === 'chat' && selected.id === NEW_CHAT_ID;
    return { key: isNew ? null : selected.key, view: null, provisional: isNew };
  }
  if (pending) return { key: conversationRefKey(pending), view: null, provisional: true };
  return { key: null, view, provisional: false, ...(view === CENTRE_VIEW.design ? { design } : {}) };
};

export const locationMatches = (location: IAppLocation, wanted: IWantedLocation): boolean =>
  (location.ref ? conversationRefKey(location.ref) : null) === wanted.key &&
  location.view === wanted.view &&
  sameDesignLocation(location.design ?? DESIGN_HOME, wanted.design ?? DESIGN_HOME);

const sameConversation = (a: TConversationRef, b: TConversationRef): boolean =>
  a.kind === b.kind &&
  a.id === b.id &&
  (a.kind === 'chat' || b.kind === 'chat' || !a.machineId || !b.machineId || a.machineId === b.machineId);

/** The address names the wanted conversation less precisely (`agent:<tab>` for `agent:<machine>:<tab>`): same page, tidied up. */
export const locationLooselyMatches = (location: IAppLocation, wanted: IWantedLocation): boolean => {
  const ref = wanted.key ? parseConversationRef(wanted.key) : null;
  return Boolean(location.ref && ref && sameConversation(location.ref, ref));
};

/** The query string the address gets: the app's own parameters replaced by the wanted ones, the rest kept. */
export const searchForWanted = (search: string, wanted: IWantedLocation): string => {
  const params = new URLSearchParams(search);
  for (const name of OWNED_PARAMS) params.delete(name);
  if (wanted.key) params.set(PARAM.conversation, wanted.key);
  else if (wanted.view) {
    params.set(PARAM.view, wanted.view);
    if (wanted.design?.project) params.set(PARAM.designProject, wanted.design.project);
    if (wanted.design?.project && wanted.design.machine)
      params.set(PARAM.designMachine, wanted.design.machine);
    if (wanted.design?.project && wanted.design.board) {
      params.set(PARAM.designBoard, wanted.design.board);
      if (wanted.design.full) params.set(PARAM.designFull, FULL_ON);
    }
  }
  const text = params.toString().replace(/%3A/gi, ':');
  return text ? `?${text}` : '';
};

/** The list entry a reference points at. A reference without a machine takes the home machine's tab first. */
export const findConversationByRef = (
  conversations: readonly Conversation[],
  ref: TConversationRef,
  defaultMachineId?: string,
): Conversation | undefined => {
  if (ref.kind === 'chat') return conversations.find((item) => item.kind === 'chat' && item.id === ref.id);
  const tabs = conversations.filter((item) => item.kind === 'agent' && item.id === ref.id);
  if (!defaultMachineId) return tabs[0];
  if (ref.machineId) return tabs.find((item) => item.machineId === ref.machineId);
  return tabs.find((item) => item.machineId === defaultMachineId) ?? tabs[0];
};

export const placeholderChat = (id: string): Conversation => ({
  key: chatConversationKey(id),
  id,
  title: t('lib.hlConversationUrl.placeholderTitle'),
  updatedAt: 0,
  provider: 'chat',
  kind: 'chat',
});
