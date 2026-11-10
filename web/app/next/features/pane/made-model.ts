// What the «Сделано в этом чате» block draws, off the runtime's list and the chat's pictures: no React, no network.
// Pictures come from the snapshot the page already holds; documents, boards and designs from GET …/made.
import {
  parsePage,
  type ChatMade,
  type MadeItem,
  type MadeTaskRef,
  type Picture,
  type Shot,
} from '@/next/protocol/v2';
import { plural, t } from '@/next/i18n';
import { stateText } from '@/next/features/apps/model';
import type { IMadeCard, IMadeKid } from '@/next/features/pane/made-block';

export interface IMadeImage {
  id: string;
  name: string;
  /** When it was looked at; null for a picture only named in a message. */
  at: number | null;
  /** The subagent that looked at it; null for the agent itself. */
  agent: string | null;
}

const baseName = (path: string): string => path.slice(path.lastIndexOf('/') + 1);

/**
 * Shots (newest first), then the pictures messages named (the latest last on the wire, so turned about); a picture
 * whose file is already a shot is not shown twice.
 */
export const mergeImages = (shots: readonly Shot[], pictures: readonly Picture[]): IMadeImage[] => {
  const named = new Set(shots.map((x) => x.name));
  const seen = new Set(shots.map((x) => x.id));
  const out: IMadeImage[] = shots.map((x) => ({ id: x.id, name: x.name, at: x.at, agent: x.agent ?? null }));
  for (const picture of [...pictures].reverse()) {
    const name = baseName(picture.text);
    if (seen.has(picture.id) || named.has(name)) continue;
    seen.add(picture.id);
    out.push({ id: picture.id, name, at: null, agent: null });
  }
  return out;
};

/** The id of an item among the cards. */
export const madeKey = (item: MadeItem): string => `${item.kind}:${item.ref}`;

const ICON_OF: Record<MadeItem['kind'], string> = {
  doc: 'file-text',
  board: 'layout-dashboard',
  design: 'palette',
  task: 'square-check-big',
};

export const iconOf = (item: MadeItem): string => {
  if (item.kind === 'design' && item.designKind === 'slides') return 'presentation';
  if (item.kind === 'task' && isEpic(item)) return 'layers';
  return ICON_OF[item.kind];
};

/** An epic of the tracker. */
export const isEpic = (item: MadeItem): boolean => item.task?.type === 'Epic';

/** The key of a task (HRN-515): the tracker says it, else it is the end of the task's address. */
export const taskKey = (item: MadeItem): string =>
  item.task?.key ?? item.ref.slice(item.ref.lastIndexOf('/') + 1);

/** The host of a link, short; empty for a design or text that is not a link. */
export const hostOf = (item: MadeItem): string => {
  if (item.kind === 'design' || item.kind === 'task') return '';
  try {
    return new URL(item.ref).host;
  } catch {
    return '';
  }
};

// A page is one thing whatever address it was met by: the portal's `/a/<slug>`, a board's `/b/<slug>?k=…`, the platform's
// reader `/…/docs/<slug>`. The slug (with the kind) names it; any other address is itself without its query and hash.
const PAGE_PATH = /^\/(?:.*\/)?(a|b|docs)\/([\w-]+)\/?$/;

/** What makes two items one: the same page by its slug, the same address, the same design. */
export const sameKey = (item: MadeItem): string => {
  if (item.kind === 'design') return `design:${item.ref}`;
  if (item.kind === 'task') return `task:${taskKey(item)}`;
  try {
    const url = new URL(item.ref);
    const page = PAGE_PATH.exec(url.pathname);
    if (page) return `${item.kind}:${page[1] === 'b' ? 'b' : 'a'}:${page[2]}`;
    return `${item.kind}:${url.host}${url.pathname.replace(/\/+$/, '')}`;
  } catch {
    return `${item.kind}:${item.ref}`;
  }
};

/**
 * Each thing once, in the order it first stands (newest first, as the runtime sends it). Of two that are one, the
 * newest time, a title once known and a board's key once seen are kept.
 */
export const dedupeMade = (items: readonly MadeItem[]): MadeItem[] => {
  const at = new Map<string, number>();
  const out: MadeItem[] = [];
  for (const item of items) {
    if (isSiteRoot(item)) continue;
    const key = sameKey(item);
    const had = at.get(key);
    if (had === undefined) {
      at.set(key, out.length);
      out.push(item);
      continue;
    }
    const have = out[had];
    const keyed = (ref: string) => ref.includes('?k=');
    out[had] = {
      ...have,
      at: Math.max(have.at, item.at) as MadeItem['at'],
      title: have.title !== '' ? have.title : item.title,
      ref: keyed(item.ref) && !keyed(have.ref) ? item.ref : have.ref,
      op: item.op === 'created' ? item.op : have.op,
    };
  }
  return out;
};

/** A bare address with no page in it (the portal's own front door): opened once, never made, not worth a row. */
export const isSiteRoot = (item: MadeItem): boolean => {
  if (item.kind === 'design' || item.kind === 'task') return false;
  try {
    const url = new URL(item.ref);
    return url.pathname.replace(/\/+$/, '') === '' && url.search === '';
  } catch {
    return false;
  }
};

/** The word for a kind of thing made, when it has no name. */
export const kindWord = (item: MadeItem): string => {
  if (item.kind === 'doc') return t('next.made.doc');
  if (item.kind === 'board') return t('next.made.board');
  if (item.kind === 'task') return isEpic(item) ? t('next.made.epic') : t('next.made.task');
  return t('next.made.design');
};

/** Where a page lives, short: its host and, for a page named by a slug, the slug (what tells two unnamed ones apart). */
export const whereOf = (item: MadeItem): string => {
  if (item.kind === 'task') return taskKey(item);
  const host = hostOf(item);
  const slug = PAGE_PATH.exec(pathOf(item.ref))?.[2];
  return slug ? `${host}/${slug}` : host;
};

const pathOf = (ref: string): string => {
  try {
    return new URL(ref).pathname;
  } catch {
    return '';
  }
};

/** Under a card's name: the host of a link, the kind of a design. */
export const aboutOf = (item: MadeItem): string => {
  if (item.kind === 'task') return item.task?.state ? stateText(item.task.state) : '';
  if (item.kind !== 'design') return hostOf(item);
  if (item.designKind === 'site') return t('next.made.site');
  if (item.designKind === 'slides') return t('next.made.slides');
  if (item.designKind === 'ds') return t('next.made.ds');
  return '';
};

// ── Tasks: an epic is one row that opens into its tasks ──

/** From this many tasks in a chat they are told by their epic; fewer stand one to a row. */
export const TASKS_GROUPED_FROM = 4;

/** Where a card of the made block opens: what a press on it (or on one of its tasks) hands the pane. */
export type MadeTarget = Pick<MadeItem, 'kind' | 'ref'>;

export interface IMadeEntry {
  /** When the newest thing the card stands for was touched: the card stands among the others by it. */
  at: number;
  card: IMadeCard;
  targets: [string, MadeTarget][];
}

const kidOf = (x: MadeTaskRef): IMadeKid => ({
  id: `task:${x.url}`,
  key: x.key,
  title: x.title !== '' ? x.title : x.key,
  state: x.state !== '' ? stateText(x.state) : '',
  done: x.resolved,
});

/**
 * The cards the chat's tasks make. A few tasks are a row each (an epic opens into its tasks). Past TASKS_GROUPED_FROM the
 * tasks are told by their epic: one row for the epic, opened into the tasks of it the chat touched and, when the chat
 * touched the epic itself, all of its tasks; a task without an epic stays a row of its own.
 */
export const taskEntries = (items: readonly MadeItem[]): IMadeEntry[] => {
  const tasks = items.filter((x) => x.kind === 'task');
  if (tasks.length === 0) return [];
  const row = (item: MadeItem, kids: readonly IMadeKid[] | undefined, at: number): IMadeEntry => {
    const key = taskKey(item);
    const id = madeKey(item);
    return {
      at,
      card: {
        id,
        icon: iconOf(item),
        title: item.title !== '' ? item.title : key,
        pending: false,
        about: kids?.length ? plural(kids.length, 'next.made.tasks') : aboutOf(item),
        kids,
      },
      targets: [[id, { kind: 'task', ref: item.ref }]],
    };
  };
  const entry = (item: MadeItem): IMadeEntry => {
    const kids = isEpic(item) && item.task ? item.task.children.map(kidOf) : undefined;
    const made = row(item, kids, item.at);
    for (const x of item.task?.children ?? [])
      made.targets.push([`task:${x.url}`, { kind: 'task', ref: x.url }]);
    return made;
  };
  if (tasks.length < TASKS_GROUPED_FROM) return tasks.map(entry);

  const groups = new Map<
    string,
    { head: MadeItem | null; parent: MadeTaskRef | null; members: MadeItem[] }
  >();
  const alone: MadeItem[] = [];
  for (const item of tasks) {
    const epic = isEpic(item) ? taskKey(item) : (item.task?.parent?.key ?? null);
    if (epic === null) {
      alone.push(item);
      continue;
    }
    const group = groups.get(epic) ?? { head: null, parent: null, members: [] };
    if (isEpic(item)) group.head = item;
    else {
      group.members.push(item);
      group.parent = item.task?.parent ?? null;
    }
    groups.set(epic, group);
  }
  const out = alone.map(entry);
  for (const [epic, group] of groups) {
    const head = group.head;
    const url = head?.ref ?? group.parent?.url ?? '';
    const all = new Map<string, IMadeKid>();
    for (const x of head?.task?.children ?? []) all.set(x.key, kidOf(x));
    for (const x of group.members) {
      const key = taskKey(x);
      all.set(key, {
        id: madeKey(x),
        key,
        title: x.title !== '' ? x.title : key,
        state: x.task?.state ? stateText(x.task.state) : '',
        done: x.task?.resolved ?? false,
      });
    }
    const kids = [...all.values()];
    const at = Math.max(head?.at ?? 0, ...group.members.map((x) => x.at));
    const id = head ? madeKey(head) : `task:${url}`;
    const title = head ? (head.title !== '' ? head.title : epic) : group.parent?.title || epic;
    const targets: [string, MadeTarget][] = [[id, { kind: 'task', ref: url }]];
    for (const x of head?.task?.children ?? []) targets.push([`task:${x.url}`, { kind: 'task', ref: x.url }]);
    for (const x of group.members) targets.push([madeKey(x), { kind: 'task', ref: x.ref }]);
    out.push({
      at,
      card: {
        id,
        icon: 'layers',
        title,
        pending: false,
        about: plural(kids.length, 'next.made.tasks'),
        kids,
      },
      targets,
    });
  }
  return out.sort((a, b) => b.at - a.at);
};

// ── Kept for the next load of the page, so the block draws at once while the runtime answers ──

const KEY = 'hx-next:made:v1';
const KEPT_CHATS = 12;

const storage = (): Storage | null => {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
};

const readAll = (): Record<string, unknown> => {
  try {
    const raw = storage()?.getItem(KEY);
    const doc: unknown = raw ? JSON.parse(raw) : null;
    return typeof doc === 'object' && doc !== null && !Array.isArray(doc)
      ? (doc as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
};

const memory = new Map<string, ChatMade>();
/** Chats storage was asked about and had nothing: not read again on every draw. */
const missed = new Set<string>();

/** The list last seen in the chat; null when there is none (or what was kept is not a list). */
export const cachedMade = (conv: string): ChatMade | null => {
  const here = memory.get(conv);
  if (here) return here;
  if (missed.has(conv)) return null;
  const kept = readAll()[conv];
  if (kept === undefined) {
    missed.add(conv);
    return null;
  }
  try {
    const made = parsePage.made(kept);
    memory.set(conv, made);
    return made;
  } catch {
    missed.add(conv);
    return null;
  }
};

/** Remembers the list of a chat; the least recently written chats are forgotten. */
export const rememberMade = (conv: string, made: ChatMade): void => {
  memory.set(conv, made);
  missed.delete(conv);
  try {
    const all = readAll();
    delete all[conv];
    const kept = Object.entries({ ...all, [conv]: made }).slice(-KEPT_CHATS);
    storage()?.setItem(KEY, JSON.stringify(Object.fromEntries(kept)));
  } catch {
    // A full or private storage: the list is kept for this page only.
  }
};

/** Forgets what was kept (tests). */
export const resetMade = (): void => {
  memory.clear();
  missed.clear();
  try {
    storage()?.removeItem(KEY);
  } catch {
    // Nothing kept.
  }
};
