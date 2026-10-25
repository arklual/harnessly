// Where the «+» pickers find what can be attached to a message: the designs of the machine's library (recent first,
// with their picture), the team's documents of the catalogue and the Markdown of the project, the boards, the files of
// the chat's folder (a quick open over its tree), a merge request or a task by its link or key. Each source answers a
// query with rows; the row's `ref` is what the message carries (protocol MessageRef).
import { platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import { fetchLibrary, keptLibrary, thumbPath, type IDesign } from '@/next/features/designly/api';
import type { MachineId, MessageRef, RefKind } from '@/next/protocol/v2';
import { appConfig } from '@/platform/config';

export const ATTACH_KIND = {
  project: 'project',
  design: 'design',
  doc: 'doc',
  board: 'board',
  link: 'link',
} as const;
export type TAttachKind = (typeof ATTACH_KIND)[keyof typeof ATTACH_KIND];

export interface IAttachRow {
  /** Tells the rows apart (the ref's kind and id). */
  key: string;
  ref: MessageRef;
  title: string;
  /** A quiet second line: a folder, a date, where it is. */
  hint: string | null;
  /** A picture of it (a design's first board); null draws the kind's icon. */
  thumb: string | null;
}

export interface IAttachScope {
  machine: MachineId;
  /** The chat's folder: the project files and the project's documents are searched there. */
  cwd: string | null;
}

/** A source answers a query; a link source answers only what the person typed. */
export type TAttachLoad = (query: string, signal: AbortSignal) => Promise<IAttachRow[]>;

const ROWS_MAX = 50;
/** A list read once serves the keys typed after it for this long: the search filters it on the page. */
export const LIST_FRESH_MS = 30_000;

const lists = new Map<string, { at: number; value: Promise<unknown> }>();

/** The list under key, read again only when it is older than LIST_FRESH_MS or the last read failed. */
const kept = <T>(key: string, read: () => Promise<T>): Promise<T> => {
  const have = lists.get(key);
  if (have && Date.now() - have.at < LIST_FRESH_MS) return have.value as Promise<T>;
  const value = read();
  lists.set(key, { at: Date.now(), value });
  value.catch(() => {
    if (lists.get(key)?.value === value) lists.delete(key);
  });
  return value;
};

/** Forgets the lists read so far (tests). */
export const forgetAttachLists = (): void => lists.clear();

const rowOf = (ref: MessageRef, hint: string | null, thumb: string | null = null): IAttachRow => ({
  key: `${ref.kind}:${ref.id}`,
  ref,
  title: ref.title,
  hint,
  thumb,
});

const matches = (query: string, ...words: (string | null | undefined)[]): boolean => {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const hay = words.filter(Boolean).join(' ').toLowerCase();
  return q.split(/\s+/).every((part) => hay.includes(part));
};

const baseName = (path: string): string => path.split('/').filter(Boolean).pop() ?? path;
const folderOf = (path: string): string | null => {
  const at = path.lastIndexOf('/');
  return at > 0 ? path.slice(0, at) : null;
};

const dayOf = (at: number | string | null): string | null => {
  if (at === null || at === '') return null;
  const date = new Date(at);
  return Number.isNaN(date.getTime())
    ? null
    : date.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
};

// ── designs ──

export const designRow = (machine: MachineId, design: IDesign): IAttachRow => {
  const board = design.boards[0];
  return rowOf(
    { kind: 'design', id: design.id, title: design.title, target: '' },
    dayOf(design.updatedAt),
    board ? thumbPath(machine, design.id, board.path, design.updatedAt) : null,
  );
};

/** A design's picture, when the library this page last read has it. */
export const designThumb = (machine: MachineId, id: string): string | null => {
  const design = keptLibrary(machine)?.designs.find((d) => d.id === id);
  return design ? designRow(machine, design).thumb : null;
};

const loadDesigns =
  ({ machine }: IAttachScope): TAttachLoad =>
  async (query, signal) => {
    const library = await kept(`design:${machine}`, () => fetchLibrary(machine));
    signal.throwIfAborted();
    return [...library.designs]
      .sort((a, b) => b.updatedAt - a.updatedAt)
      .filter((d) => matches(query, d.title, d.id))
      .slice(0, ROWS_MAX)
      .map((d) => designRow(machine, d));
  };

// ── the project's files ──

const contextFiles = async (scope: IAttachScope, query: string, signal: AbortSignal): Promise<string[]> => {
  if (!scope.cwd) return [];
  const path =
    `/context-files?cwd=${encodeURIComponent(scope.cwd)}&q=${encodeURIComponent(query.trim())}` as const;
  const body = await runtimeJson<{ files?: unknown }>(scope.machine, platformPath(path), { signal });
  return Array.isArray(body.files) ? body.files.filter((f): f is string => typeof f === 'string') : [];
};

const fileRow = (path: string, kind: RefKind): IAttachRow =>
  rowOf({ kind, id: path, title: baseName(path), target: path }, folderOf(path));

const loadFiles =
  (scope: IAttachScope): TAttachLoad =>
  async (query, signal) =>
    (await contextFiles(scope, query, signal)).map((path) => fileRow(path, 'file'));

// ── documents: the catalogue's and the project's Markdown ──

interface ICatalogDoc {
  slug: string;
  title: string;
  url: string;
  lastActivityAt?: string;
}

const isDoc = (v: unknown): v is ICatalogDoc =>
  typeof v === 'object' &&
  v !== null &&
  typeof (v as ICatalogDoc).slug === 'string' &&
  typeof (v as ICatalogDoc).url === 'string';

const MARKDOWN = /\.(md|mdx|markdown)$/i;

const catalogDocs = async (machine: MachineId): Promise<ICatalogDoc[]> => {
  if (!appConfig().artifactsUrl) return [];
  const body = await kept(`doc:${machine}`, () =>
    runtimeJson<{ docs?: unknown }>(machine, platformPath('/docs')),
  );
  return Array.isArray(body.docs) ? body.docs.filter(isDoc) : [];
};

const loadDocs =
  (scope: IAttachScope): TAttachLoad =>
  async (query, signal) => {
    const [catalog, local] = await Promise.all([
      catalogDocs(scope.machine),
      contextFiles(scope, `${query.trim()}.md`, signal).catch(() => []),
    ]);
    const team = catalog
      .filter((d) => matches(query, d.title, d.slug))
      .sort((a, b) => (b.lastActivityAt ?? '').localeCompare(a.lastActivityAt ?? ''))
      .map((d) =>
        rowOf(
          { kind: 'doc', id: d.url, title: d.title || d.slug, target: d.url },
          dayOf(d.lastActivityAt ?? null),
        ),
      );
    const project = local.filter((path) => MARKDOWN.test(path)).map((path) => fileRow(path, 'doc'));
    return [...team, ...project].slice(0, ROWS_MAX);
  };

// ── boards ──

interface IBoard {
  slug: string;
  title: string;
  url: string;
  updatedAt?: string;
}

const isBoard = (v: unknown): v is IBoard =>
  typeof v === 'object' &&
  v !== null &&
  typeof (v as IBoard).slug === 'string' &&
  typeof (v as IBoard).url === 'string';

const loadBoards =
  ({ machine }: IAttachScope): TAttachLoad =>
  async (query, signal) => {
    if (!appConfig().boardsUrl) return [];
    const body = await kept(`board:${machine}`, () =>
      runtimeJson<{ boards?: unknown }>(machine, '/api/integrations/boards'),
    );
    signal.throwIfAborted();
    const boards = Array.isArray(body.boards) ? body.boards.filter(isBoard) : [];
    return boards
      .filter((b) => matches(query, b.title, b.slug))
      .sort((a, b) => (b.updatedAt ?? '').localeCompare(a.updatedAt ?? ''))
      .slice(0, ROWS_MAX)
      .map((b) =>
        rowOf(
          { kind: 'board', id: b.url, title: b.title || b.slug, target: b.url },
          dayOf(b.updatedAt ?? null),
        ),
      );
  };

// ── a merge request or a task by its link or key ──

const MR_LINK = /^https?:\/\/[^/\s]+\/(.+?)(?:\/-)?\/(merge_requests|pull)\/(\d+)(?:[/?#]\S*)?$/;
const ISSUE_LINK = /^https?:\/\/([^/\s]+)\/issue\/([A-Za-z][A-Za-z0-9]*-\d+)(?:[/?#]\S*)?$/;

const taskUrl = (key: string): string => {
  const pattern = appConfig().trackerIssueUrl;
  return pattern ? pattern.replace('{key}', encodeURIComponent(key)) : key;
};

/** What the person typed as a merge request or a task; null when it is neither. */
export const linkRef = (typed: string): MessageRef | null => {
  const text = typed.trim();
  const mr = MR_LINK.exec(text);
  if (mr) {
    const project = mr[1].split('/').pop() ?? mr[1];
    return {
      kind: 'mr',
      id: text,
      title: `${project} ${mr[2] === 'pull' ? '#' : '!'}${mr[3]}`,
      target: text,
    };
  }
  const issue = ISSUE_LINK.exec(text);
  if (issue) return { kind: 'task', id: issue[2].toUpperCase(), title: issue[2].toUpperCase(), target: text };
  const key = new RegExp(`^(?:${appConfig().taskKeyPattern})$`, 'i');
  if (key.test(text)) {
    const id = text.toUpperCase();
    return { kind: 'task', id, title: id, target: taskUrl(id) };
  }
  return null;
};

const loadLink = (): TAttachLoad => async (query) => {
  const ref = linkRef(query);
  return ref ? [rowOf(ref, ref.target === ref.id ? null : ref.target)] : [];
};

/** The source of each picker of «+». */
export const attachSource = (kind: TAttachKind, scope: IAttachScope): TAttachLoad => {
  switch (kind) {
    case ATTACH_KIND.design:
      return loadDesigns(scope);
    case ATTACH_KIND.doc:
      return loadDocs(scope);
    case ATTACH_KIND.board:
      return loadBoards(scope);
    case ATTACH_KIND.project:
      return loadFiles(scope);
    case ATTACH_KIND.link:
      return loadLink();
  }
};
