// The widgets under the chat's right column on a computer: small cards the person adds from
// «+ Виджет», moves by hand, folds and removes. The list is the person's, not the device's: it is kept in their settings
// (stores/side-widgets.ts, the synced choices of HRN-1493), so every computer shows the same. Pure: the stack draws what
// this says and saves what it asks.

/** What a widget is: harnessly's own, or a view of an installed app with the `side` target. */
export const WIDGET_KIND = {
  limits: 'limits',
  machine: 'machine',
  subagents: 'subagents',
  pipeline: 'pipeline',
  note: 'note',
  clock: 'clock',
  app: 'app',
} as const;
export type TWidgetKind = (typeof WIDGET_KIND)[keyof typeof WIDGET_KIND];

/** harnessly's own widgets, in the gallery's order. */
export const BUILTIN_WIDGETS = [
  WIDGET_KIND.limits,
  WIDGET_KIND.machine,
  WIDGET_KIND.subagents,
  WIDGET_KIND.pipeline,
  WIDGET_KIND.note,
  WIDGET_KIND.clock,
] as const;
export type TBuiltinWidget = (typeof BUILTIN_WIDGETS)[number];

/** What a widget shows the same in every copy is in the stack once; a note and a clock may be there several times. */
const ONCE: readonly TWidgetKind[] = [
  WIDGET_KIND.limits,
  WIDGET_KIND.machine,
  WIDGET_KIND.subagents,
  WIDGET_KIND.pipeline,
];

/** Whether the gallery still offers one of harnessly's own widgets. */
export const canAdd = (state: ISideWidgets, kind: TBuiltinWidget): boolean =>
  !ONCE.includes(kind) || !state.items.some((x) => x.kind === kind);

const KINDS: readonly string[] = Object.values(WIDGET_KIND);

/** Where a widget shows: in every chat, or in the chats listed. */
export type TWidgetChats = 'all' | readonly string[];

export interface ISideWidget {
  id: string;
  kind: TWidgetKind;
  /**
   * The person's word on where it shows. Absent: the widget's own (an app's widget an agent made for a chat shows in
   * that chat, every other one in every chat).
   */
  chats?: TWidgetChats;
  folded?: boolean;
  /** An app's widget: the app and its view. */
  app?: string;
  view?: string;
  /** «Заметка»: what the person wrote. */
  note?: string;
  /** «Часы»: when the timer the person started rings (epoch ms); none, no timer. */
  endsAt?: number;
}

export interface ISideWidgets {
  items: readonly ISideWidget[];
  /** The apps' widgets (`<app>/<view>`) the stack has met: one met for the first time (an agent made it) is added once. */
  seen: readonly string[];
}

export const NO_WIDGETS: ISideWidgets = { items: [], seen: [] };

/** The most widgets the stack holds, and the longest note. */
export const MAX_WIDGETS = 12;
export const MAX_NOTE = 2000;
const MAX_SEEN = 100;
/** The most chats one widget is pinned to. */
const MAX_CHATS = 50;

export const appWidgetKey = (app: string, view: string): string => `${app}/${view}`;

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);

const readItem = (v: unknown): ISideWidget | null => {
  if (!isRecord(v)) return null;
  const id = str(v.id);
  const kind = str(v.kind);
  if (!id || !kind || !KINDS.includes(kind)) return null;
  const item: ISideWidget = { id, kind: kind as TWidgetKind };
  if (v.folded === true) item.folded = true;
  if (v.chats === 'all') item.chats = 'all';
  else if (Array.isArray(v.chats)) {
    const chats = v.chats.filter((c): c is string => typeof c === 'string' && c !== '').slice(0, MAX_CHATS);
    if (chats.length > 0) item.chats = chats;
  }
  if (kind === WIDGET_KIND.app) {
    const app = str(v.app);
    const view = str(v.view);
    if (!app || !view) return null;
    item.app = app;
    item.view = view;
  }
  const note = str(v.note);
  if (kind === WIDGET_KIND.note && note) item.note = note.slice(0, MAX_NOTE);
  if (kind === WIDGET_KIND.clock && typeof v.endsAt === 'number' && Number.isFinite(v.endsAt))
    item.endsAt = v.endsAt;
  return item;
};

/** The saved list; a broken record or a broken item is left out. */
export const parseWidgets = (raw: string | null): ISideWidgets => {
  if (!raw) return NO_WIDGETS;
  try {
    const saved: unknown = JSON.parse(raw);
    if (!isRecord(saved)) return NO_WIDGETS;
    const items: ISideWidget[] = [];
    for (const v of Array.isArray(saved.items) ? saved.items : []) {
      const item = readItem(v);
      if (item && !items.some((x) => x.id === item.id) && items.length < MAX_WIDGETS) items.push(item);
    }
    const seen = (Array.isArray(saved.seen) ? saved.seen : [])
      .filter((s): s is string => typeof s === 'string')
      .slice(-MAX_SEEN);
    return { items, seen };
  } catch {
    return NO_WIDGETS;
  }
};

export const serializeWidgets = (state: ISideWidgets): string => JSON.stringify(state);

const freshId = (state: ISideWidgets, base: string): string => {
  const taken = new Set(state.items.map((x) => x.id));
  if (!taken.has(base)) return base;
  for (let n = 2; ; n++) if (!taken.has(`${base}-${n}`)) return `${base}-${n}`;
};

/** A widget added at the end; null when the stack is full. An app's widget, or one of ONCE, already there is not added twice. */
export const withAdded = (
  state: ISideWidgets,
  kind: TWidgetKind,
  app?: { app: string; view: string },
): ISideWidgets | null => {
  if (state.items.length >= MAX_WIDGETS) return null;
  if (kind === WIDGET_KIND.app) {
    if (!app) return null;
    const key = appWidgetKey(app.app, app.view);
    if (state.items.some((x) => x.app === app.app && x.view === app.view)) return null;
    return {
      items: [...state.items, { id: freshId(state, `app:${key}`), kind, app: app.app, view: app.view }],
      seen: state.seen.includes(key) ? state.seen : [...state.seen, key].slice(-MAX_SEEN),
    };
  }
  if (!canAdd(state, kind)) return null;
  return { ...state, items: [...state.items, { id: freshId(state, kind), kind }] };
};

export const withRemoved = (state: ISideWidgets, id: string): ISideWidgets => ({
  ...state,
  items: state.items.filter((x) => x.id !== id),
});

const withItem = (state: ISideWidgets, id: string, patch: (x: ISideWidget) => ISideWidget): ISideWidgets => ({
  ...state,
  items: state.items.map((x) => (x.id === id ? patch(x) : x)),
});

export const withFolded = (state: ISideWidgets, id: string, folded: boolean): ISideWidgets =>
  withItem(state, id, (x) => {
    const { folded: _was, ...rest } = x;
    return folded ? { ...rest, folded: true } : rest;
  });

export const withNote = (state: ISideWidgets, id: string, note: string): ISideWidgets =>
  withItem(state, id, (x) => {
    const { note: _was, ...rest } = x;
    const text = note.slice(0, MAX_NOTE);
    return text ? { ...rest, note: text } : rest;
  });

export const withTimer = (state: ISideWidgets, id: string, endsAt: number | null): ISideWidgets =>
  withItem(state, id, (x) => {
    const { endsAt: _was, ...rest } = x;
    return endsAt === null ? rest : { ...rest, endsAt };
  });

/** A widget carried by hand next to another; null when nothing moves. */
export const withMoved = (
  state: ISideWidgets,
  id: string,
  at: string,
  after: boolean,
): ISideWidgets | null => {
  if (id === at) return null;
  const item = state.items.find((x) => x.id === id);
  if (!item || !state.items.some((x) => x.id === at)) return null;
  const rest = state.items.filter((x) => x.id !== id);
  const i = rest.findIndex((x) => x.id === at);
  rest.splice(after ? i + 1 : i, 0, item);
  return rest.every((x, k) => x.id === state.items[k]?.id) ? null : { ...state, items: rest };
};

/**
 * The apps' widgets the stack meets for the first time (an agent made one with `harnessly widget create`) are added
 * at the end, once: one the person removed stays removed. null when there is nothing new.
 */
export const withNewApps = (
  state: ISideWidgets,
  found: readonly { app: string; view: string }[],
): ISideWidgets | null => {
  let next = state;
  for (const f of found) {
    const key = appWidgetKey(f.app, f.view);
    if (next.seen.includes(key)) continue;
    next = withAdded(next, WIDGET_KIND.app, f) ?? { ...next, seen: [...next.seen, key].slice(-MAX_SEEN) };
  }
  return next === state ? null : next;
};

/** Where the stack is drawn: the chat on screen (null: none, the start page), and the chat an app's widget was made for. */
export interface IWidgetPlace {
  chat: string | null;
  /** The chat a view's widget block names, if it names one (an agent made it for that chat). */
  madeFor: (app: string, view: string) => string | undefined;
}

/** Where a widget shows now: the person's word, else the widget's own. */
export const chatsOf = (x: ISideWidget, place: IWidgetPlace): TWidgetChats => {
  if (x.chats) return x.chats;
  const made = x.kind === WIDGET_KIND.app ? place.madeFor(x.app ?? '', x.view ?? '') : undefined;
  return made ? [made] : 'all';
};

/** Whether a widget shows in the chat on screen. */
export const shownHere = (x: ISideWidget, place: IWidgetPlace): boolean => {
  const chats = chatsOf(x, place);
  return chats === 'all' || (place.chat !== null && chats.includes(place.chat));
};

/**
 * What the stack draws in this chat. Apps' widgets whose app is no longer installed are not drawn (they stay saved: the
 * app may come back); while the apps are not known yet (`installed` is null) they are drawn as placeholders.
 */
export const shownWidgets = (
  state: ISideWidgets,
  installed: ((app: string, view: string) => boolean) | null,
  place: IWidgetPlace = { chat: null, madeFor: () => undefined },
): ISideWidget[] =>
  state.items.filter(
    (x) =>
      shownHere(x, place) &&
      (x.kind !== WIDGET_KIND.app || installed === null || installed(x.app ?? '', x.view ?? '')),
  );

/** The widget shows in every chat (the person's word). */
export const withAllChats = (state: ISideWidgets, id: string): ISideWidgets =>
  withItem(state, id, (x) => ({ ...x, chats: 'all' }));

/** The widget shows in this chat only. */
export const withOnlyChat = (state: ISideWidgets, id: string, chat: string): ISideWidgets =>
  withItem(state, id, (x) => ({ ...x, chats: [chat] }));

/** The widget shows in this chat too: pinned from the gallery of a chat it was not in. */
export const withPinnedChat = (
  state: ISideWidgets,
  id: string,
  chat: string,
  place: IWidgetPlace,
): ISideWidgets => {
  const item = state.items.find((x) => x.id === id);
  if (!item) return state;
  const chats = chatsOf(item, place);
  if (chats === 'all' || chats.includes(chat)) return state;
  return withItem(state, id, (x) => ({ ...x, chats: [...chats, chat].slice(-MAX_CHATS) }));
};

/** How long is left of a timer, in whole seconds, never below zero. */
export const timerLeft = (endsAt: number, now: number): number =>
  Math.max(0, Math.ceil((endsAt - now) / 1000));

/** «12:05», «1:02:03». */
export const clockText = (seconds: number): string => {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  const two = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${h}:${two(m)}:${two(s)}` : `${m}:${two(s)}`;
};

/** The smart widget's own interval (minutes): never below five. */
export const SMART_EVERY_MIN = 5;
export const smartEveryMs = (every: number | undefined): number =>
  Math.max(SMART_EVERY_MIN, every ?? 30) * 60_000;

/** Whether a smart widget asks the model now: due by its interval, or the person pressed for it. */
export const smartDue = (lastAt: number | null, everyMs: number, now: number): boolean =>
  lastAt === null || now - lastAt >= everyMs;
