// Designly over v2 (/api/v2/designly/*): the library lists the machine's Claude Design projects with their boards,
// kinds and the conversations drawing them; a new project opens a conversation of an agent in its folder. The pictures
// come from the runtime's library route; a board opens in a preview slot of its own origin.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
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

export const pinDesign = (m: MachineId, id: string, pinned: boolean) =>
  page(m, '/designly/pin', parsePage.designlyPin, { method: 'POST', body: { id, pinned } });

/** The picture of a board for a card; `version` is the cache key (the project's last change). */
export const thumbPath = (m: MachineId, id: string, board: string, version: number) =>
  runtimePath(
    platformPath(
      `/design/library/thumb?artifact=${encodeURIComponent(id)}&board=${encodeURIComponent(board)}&v=${version}`,
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
