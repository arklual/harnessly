// Designly over v2 (/api/v2/designly/*): the library lists the machine's Claude Design projects with their boards,
// kinds and the conversations drawing them; a new project opens a conversation of an agent in its folder. The pictures
// come from the runtime's library route; a board opens in a preview slot of its own origin.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { readCheck, readPageLog } from '@/next/features/designly/server-data';
import { wordedPage } from '@/next/core/net/worded';
import {
  parseDesignlyRefusal,
  parsePage,
  type DesignArtifact,
  type DesignlyCreate,
  type DesignSystemInfo,
  type MachineId,
} from '@/next/protocol/v2';

export type IDesign = DesignArtifact;
export type IDesignSystem = DesignSystemInfo;

export interface IDesignLibrary {
  /** Loopback port of the library server on the machine. */
  port: number;
  /** The library's folder: a project's conversations work in `<root>/<id>`. */
  root: string;
  designs: IDesign[];
}

const page = <T>(
  m: MachineId,
  path: `/${string}`,
  read: (v: unknown) => T,
  init: { method?: 'GET' | 'POST'; body?: unknown; signal?: AbortSignal } = {},
) => wordedPage(m, path, read, parseDesignlyRefusal, init);

let kept: { machine: MachineId; library: IDesignLibrary } | null = null;

/**
 * The library as the last read of this machine left it, or null before any. Opening a design from the library needs
 * its port and its boards: the home has just read them, so the design does not ask again before it can ask for its
 * page. It is a start only; the design reads the library anew behind it.
 */
export const keptLibrary = (m: MachineId): IDesignLibrary | null =>
  kept && kept.machine === m ? kept.library : null;

export const fetchLibrary = async (m: MachineId, signal?: AbortSignal): Promise<IDesignLibrary> => {
  const library = await page(m, '/designly/library', parsePage.designlyLibrary, { signal });
  const read = { port: library.port, root: library.root, designs: library.artifacts };
  kept = { machine: m, library: read };
  return read;
};

/** A new project from the brief: the runtime copies the template and opens a conversation of an agent in it. */
export const createDesign = (m: MachineId, body: DesignlyCreate) =>
  page(m, '/designly/create', parsePage.designlyCreate, { method: 'POST', body });

/** The design systems a new project can take. */
export const fetchSystems = (m: MachineId, signal?: AbortSignal) =>
  page(m, '/designly/systems', parsePage.designlySystems, { signal });

/** What the machine has for designs: where new designs' engine comes from, a browser, ffmpeg. */
export interface IDesignEngine {
  source: 'own' | 'vendor' | 'file';
  /** The vendor's release new designs start on. */
  release: string | null;
  chrome: boolean;
  ffmpeg: boolean;
  /** The command that adds the vendor's engine, to run in a terminal of the machine (optional). */
  fetch: string;
}

export const fetchEngine = (m: MachineId, signal?: AbortSignal) =>
  runtimeJson<IDesignEngine>(m, platformPath('/design/engine'), { signal });

export const pinDesign = (m: MachineId, id: string, pinned: boolean) =>
  page(m, '/designly/pin', parsePage.designlyPin, { method: 'POST', body: { id, pinned } });

/** Copies back the demo projects (Relay) the library no longer has; edited ones stay as they are. */
export const restoreDemoDesigns = (m: MachineId) =>
  page(m, '/designly/demo', parsePage.designlyDemo, { method: 'POST', body: {} });

/** The chat conv was started from the design's own column: the machine lists it among the design's chats. */
export const recordDesignChat = (m: MachineId, id: string, conv: string) =>
  page(m, '/designly/chat', parsePage.designlyChat, { method: 'POST', body: { id, conv } });

/**
 * The design's chats as one machine has them: started from the design, then the ones that edited its files. The
 * library is read without becoming the kept one (`keptLibrary` is the open machine's).
 */
export const fetchDesignChats = async (
  m: MachineId,
  design: string,
  signal?: AbortSignal,
): Promise<string[]> => {
  const library = await page(m, '/designly/library', parsePage.designlyLibrary, { signal });
  const found = library.artifacts.find((a) => a.id === design);
  return found ? [...found.convs, ...(found.touchedBy ?? [])] : [];
};

/** The project goes to the machine's trash; resolves once the machine has moved it. */
export const deleteDesign = async (m: MachineId, id: string): Promise<void> => {
  await runtimeJson<unknown>(m, platformPath('/design/library/delete'), postJson({ project: id }));
};

export interface IFolder {
  id: string;
  name: string;
  /** '' for a top folder; a folder holds folders one level deep. */
  parent: string;
}

export interface IDesignSystemRef {
  id: string;
  label: string;
}

/** The person's own shelf: folders, where each project lies, the design system each was built on. */
export interface ILibraryLayout {
  folders: IFolder[];
  placed: Record<string, string>;
  systems: Record<string, IDesignSystemRef>;
}

export interface ITrashItem {
  id: string;
  title: string;
  deletedAt: number;
  kind: 'site' | 'slides' | 'video';
}

const layoutPost = <T>(m: MachineId, path: `/${string}`, body: object, signal?: AbortSignal) =>
  runtimeJson<T>(m, platformPath(path), { ...postJson(body), signal });

/** A runtime without folders answers nothing usable: the library then has none (an addition, not the library). */
export const fetchLayout = async (m: MachineId, signal?: AbortSignal): Promise<ILibraryLayout> => {
  const raw = await layoutPost<Partial<ILibraryLayout>>(m, '/design/library/layout', {}, signal);
  return { folders: raw.folders ?? [], placed: raw.placed ?? {}, systems: raw.systems ?? {} };
};

export const saveFolder = (m: MachineId, input: { id?: string; name: string; parent?: string }) =>
  layoutPost<IFolder>(m, '/design/library/folder', input);

export const deleteFolder = (m: MachineId, id: string, projects: 'keep' | 'trash') =>
  layoutPost<{ ok: boolean; trashed: string[] }>(m, '/design/library/folder/delete', { id, projects });

/** Lays projects in a folder; '' is the top. */
export const moveToFolder = (m: MachineId, projects: string[], folder: string) =>
  layoutPost<unknown>(m, '/design/library/folder/move', { projects, folder });

export const fetchTrash = async (m: MachineId): Promise<ITrashItem[]> =>
  (await layoutPost<{ items: ITrashItem[] }>(m, '/design/library/trash', {})).items;

export const restoreDesign = (m: MachineId, id: string) =>
  layoutPost<unknown>(m, '/design/library/trash/restore', { project: id });

export const purgeDesign = (m: MachineId, id: string) =>
  layoutPost<unknown>(m, '/design/library/trash/purge', { project: id });

/** The picture of a board for a card; `version` is the cache key (the project's last change). */
/**
 * The picture of a board. `fresh` waits for the picture of the board as it is now instead of an earlier one: for an
 * address that already moves with the board's file (a deck's strip), which an earlier picture would otherwise keep.
 */
export const thumbPath = (m: MachineId, id: string, board: string, version: number | string, fresh = false) =>
  runtimePath(
    platformPath(
      `/design/library/thumb?artifact=${encodeURIComponent(id)}&board=${encodeURIComponent(board)}&v=${encodeURIComponent(version)}${fresh ? '&fresh=1' : ''}`,
    ),
    m,
  );

const encodeSegments = (pathname: string) => pathname.split('/').map(encodeURIComponent).join('/');

/** Where a board lives on the library server. */
export const boardHref = (id: string, board: string): string => {
  const at = board.indexOf('?');
  const pathname = at < 0 ? board : board.slice(0, at);
  return `/a/${id}/p/${encodeSegments(pathname)}${at < 0 ? '' : board.slice(at)}`;
};

/**
 * A board in an open preview slot. The slot's origin may carry a path (a slot by path on the hub:
 * `https://hub/m/<machine>/__pv/<n>-<key>`), so the board's root-absolute address is appended, not resolved against it.
 */
export const slotBoardHref = (slotOrigin: string, id: string, board: string): string =>
  `${slotOrigin.replace(/\/+$/, '')}${boardHref(id, board)}`;

/** An address another device can open: not the machine's own loopback (`localhost`, `127.*`, `::1`). */
export const isPublicAddress = (href: string): boolean => {
  try {
    const host = new URL(href).hostname.replace(/^\[|\]$/g, '');
    return !(host === 'localhost' || host.endsWith('.localhost') || host === '::1' || /^127\./.test(host));
  } catch {
    return false;
  }
};

/** What the open pages of the design logged to the library (errors, long tasks, slow loads), the newest 200. */
export const fetchPageLog = async (m: MachineId, id: string, signal?: AbortSignal) => {
  const answer = await runtimeJson<{ entries: unknown }>(
    m,
    platformPath(`/design/library/log?artifact=${encodeURIComponent(id)}`),
    { signal, cache: 'no-store' },
  );
  return readPageLog(answer.entries);
};

/** The design's last check in a headless browser; null when it was never checked. */
export const fetchCheck = async (m: MachineId, id: string, signal?: AbortSignal) => {
  const answer = await runtimeJson<{ report: unknown }>(
    m,
    platformPath(`/design/library/check?artifact=${encodeURIComponent(id)}`),
    { signal, cache: 'no-store' },
  );
  return readCheck(answer.report);
};

// ---- a kit as one file (POST/GET /design/kit/import|export)

export interface IKitDiff {
  added: string[];
  changed: string[];
  removed: string[];
}

export type TKitImport =
  | {
      ok: true;
      id: string;
      version: string;
      installed: boolean;
      unchanged: boolean;
      files: number;
      restricted: number;
      warnings: string[];
    }
  | { ok: false; error: string; id: string; version: string; conflict: IKitDiff | null };

const strList = (v: unknown): string[] =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
const rec = (v: unknown): Record<string, unknown> =>
  typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};

/** Checks an uploaded kit file (`dryRun`) or installs it; a version held with other content comes back as a conflict, never overwritten. */
export const importKit = async (
  m: MachineId,
  uploadId: string,
  opt: { dryRun?: boolean; asId?: string; asVersion?: string } = {},
): Promise<TKitImport> => {
  const q = new URLSearchParams({ upload: uploadId });
  if (opt.dryRun) q.set('dryRun', '1');
  if (opt.asId) q.set('asId', opt.asId);
  if (opt.asVersion) q.set('asVersion', opt.asVersion);
  const res = await fetch(
    runtimePath(platformPath(`/design/kit/import?${q.toString()}` as `/${string}`), m),
    {
      method: 'POST',
      credentials: 'same-origin',
    },
  );
  const body = rec(await res.json().catch(() => null));
  const manifest = rec(body.manifest);
  if (!res.ok) {
    const c = rec(body.conflict);
    return {
      ok: false,
      error: typeof body.error === 'string' ? body.error : String(res.status),
      id: typeof body.id === 'string' ? body.id : '',
      version: typeof body.version === 'string' ? body.version : '',
      conflict:
        res.status === 409
          ? { added: strList(c.added), changed: strList(c.changed), removed: strList(c.removed) }
          : null,
    };
  }
  return {
    ok: true,
    id: typeof body.id === 'string' ? body.id : '',
    version: typeof body.version === 'string' ? body.version : '',
    installed: body.installed === true,
    unchanged: body.unchanged === true,
    files: Array.isArray(manifest.files) ? manifest.files.length : 0,
    restricted: Array.isArray(manifest.restricted) ? manifest.restricted.length : 0,
    warnings: strList(body.warnings),
  };
};

/** Where a kit is downloaded as one file; a kit with restricted files needs `acceptRestricted`. */
export const kitExportHref = (m: MachineId, id: string, version: string, acceptRestricted: boolean): string =>
  runtimePath(
    platformPath(
      `/design/kit/export?id=${encodeURIComponent(id)}&version=${encodeURIComponent(version)}${acceptRestricted ? '&acceptRestricted=1' : ''}` as `/${string}`,
    ),
    m,
  );
