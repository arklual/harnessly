/**
 * Library of design artifacts: Claude Design projects kept as plain folders on the machine and shown
 * in the right panel's browser, so a design is looked at, picked and commented on without publishing it anywhere.
 * Nothing here imports node modules.
 */
import type { TCliState } from '@/types/timeline';

export const LIBRARY_ID_RE = /^[a-z0-9][a-z0-9-]{0,62}$/;

export const isLibraryId = (value: string): boolean => LIBRARY_ID_RE.test(value);
/** An artifact is addressed by its folder name in the library. */
export const isArtifactRef = isLibraryId;

export const LIBRARY_LIMITS = {
  title: 120,
  boardPath: 512,
  boards: 400,
  /** An uploaded project archive; the gateway caps request bodies anyway. */
  uploadBytes: 300 * 1024 * 1024,
  /** Unpacked size and entry count of an archive: a design project is tens of megabytes and about a thousand files. */
  unpackedBytes: 1024 * 1024 * 1024,
  entries: 20_000,
} as const;

export interface ILibraryBoard {
  title: string;
  /** Under `project/`, with the query the page needs (`lib/page.html?id=hub&v=Desktop&theme=dark`). */
  path: string;
  w: number | null;
  h: number | null;
}

/** An agent conversation whose transcript touched the artifact's files. */
export interface IArtifactAgent {
  tabId: string;
  title: string;
  workspace: string | null;
  state: TCliState;
  lastEventAt: number | null;
  /** The conversation that asked: "this agent". */
  self: boolean;
}

/**
 * A chat that holds a board of the shared library: until its turn ends the board is its to edit. Held by a
 * chat of either teammate's runtime; the other runtime's chats are only text here.
 */
export interface ILibraryClaim {
  /** `boardKeyOf` of the board. */
  board: string;
  /** The person's name; the runtime's user when the machine has no owner set. */
  person: string;
  tabId: string;
  chatTitle: string;
  since: number;
  /** A chat of this runtime: its conversation can be opened from here. */
  local: boolean;
  /** Not claimed: the chat's transcript shows it writing the board. */
  inferred: boolean;
}

export interface ILibraryArtifact {
  id: string;
  title: string;
  source: string | null;
  /** Newest modification time of any file of the project, ms. */
  updatedAt: number;
  boards: ILibraryBoard[];
  /** `support.js` (the Design runtime) next to the boards; a `*.dc.html` does not draw without it. */
  hasSupport: boolean;
  /** Absolute path of `project/`: what the agent edits. */
  projectPath: string;
  /** Screenshot of the main board from the last check, as an address the panel can load; null before. */
  cover: string | null;
  /** The last check of the design, or null when it was never checked. */
  check: ILibraryCheckSummary | null;
  agents: IArtifactAgent[];
  /** Who holds or is editing which board right now. */
  claims: ILibraryClaim[];
}

export interface ILibraryCheckSummary {
  finishedAt: number | null;
  done: number;
  total: number;
  /** Boards with errors or a blank page. */
  broken: number;
  /** Boards with only failed requests. */
  warnings: number;
  failure: string | null;
}

/** GET /api/platform/design/library */
export interface ILibraryResponse {
  /** Loopback port of the library server on the machine; the panel's browser opens `localhost:<port>/…`. */
  port: number;
  root: string;
  /** The conversation the list was asked for, or null. */
  selfTabId: string | null;
  artifacts: ILibraryArtifact[];
}

/** One saved state of a board in the history of the shared library. */
export interface IBoardSnapshot {
  /** Full revision; `restore` takes it back. */
  id: string;
  at: number;
  /** Who made the change, worded for the person: "Alex · chat “Payouts”". */
  who: string;
  /** The version the board has now. */
  current: boolean;
}

/** GET /api/platform/design/library/history */
export interface IBoardHistoryResponse {
  snapshots: IBoardSnapshot[];
}

/** POST /api/platform/design/library/create */
export interface ILibraryCreateResponse {
  id: string;
  title: string;
  /** The conversation started in the new project. */
  tabId: string;
}

/** POST /api/platform/design/library/import */
export interface ILibraryImportResponse {
  id: string;
  title: string;
  boards: number;
}

const encodeSegments = (pathname: string): string => pathname.split('/').map(encodeURIComponent).join('/');

/** Where a board lives on the library server: the path is encoded per segment, the query stays as written. */
export const libraryBoardHref = (id: string, boardPath: string): string => {
  const at = boardPath.indexOf('?');
  const pathname = at < 0 ? boardPath : boardPath.slice(0, at);
  const query = at < 0 ? '' : boardPath.slice(at);
  return `/a/${id}/p/${encodeSegments(pathname)}${query}`;
};

/**
 * The picture of a board for the gallery; `version` is only the address's cache key (the project's last change).
 * `all` is the project's own page: the pictures of its other boards are drawn ahead while this one is looked at.
 */
export const libraryThumbPath = (id: string, boardPath: string, version: number, all = false): string =>
  `/api/platform/design/library/thumb?artifact=${encodeURIComponent(id)}&board=${encodeURIComponent(boardPath)}&v=${version}${all ? '&all=1' : ''}`;

/** What the panel's address field takes: host and path, no scheme. */
export const libraryBoardAddress = (port: number, id: string, boardPath: string): string =>
  `localhost:${port}${libraryBoardHref(id, boardPath)}`;

export const libraryArtifactAddress = (port: number, id: string): string => `localhost:${port}/a/${id}/`;

/** The artifact and the file a page of the library server shows: `/a/<ref>/p/<path>` → `{ ref, file }`. */
export const parseLibraryPagePath = (pagePath: string): { ref: string; file: string } | null => {
  const pathname = pagePath.split(/[?#]/, 1)[0];
  const match = pathname.match(/^\/a\/([^/]+)\/p\/(.+)$/);
  if (!match) return null;
  try {
    const ref = decodeURIComponent(match[1]);
    const file = match[2].split('/').map(decodeURIComponent).join('/');
    return isArtifactRef(ref) ? { ref, file } : null;
  } catch {
    return null;
  }
};
