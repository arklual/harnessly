// What the gateway's page for a guest link tells the guest app: the link, the design and the view its owner shared.
// It is inline JSON in <script id="guest-config" type="application/json"> (the contract is in the gateway's shell).
import { parseView, type IViewMemory } from '@/next/features/designly/view-memory';

export interface IGuestBoard {
  id: string;
  title: string;
  /** The board's path in the project, the page's key. A path may end in a query (`lib/page.html?Name`). */
  path: string;
  w: number | null;
  h: number | null;
}

export interface IGuestConfig {
  v: 1;
  token: string;
  title: string;
  rights: 'view' | 'comment';
  expiresAt: string | null;
  /** The look the owner shared (access/guest-view.ts), or null. */
  view: Record<string, unknown> | null;
  boards: IGuestBoard[];
  /** Where the project's files are, with the trailing slash. */
  filesBase: string;
  commentsUrl: string;
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

const str = (v: unknown, fallback = ''): string => (typeof v === 'string' ? v : fallback);
const size = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);

/** The config of the page, or null where there is none (the page was opened on its own). */
export const readGuestConfig = (doc: Document = document): IGuestConfig | null => {
  const raw = doc.getElementById('guest-config')?.textContent;
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed) || parsed.v !== 1) return null;
    const boards = Array.isArray(parsed.boards)
      ? parsed.boards.filter(isRecord).map((b): IGuestBoard => ({
          id: str(b.id),
          title: str(b.title),
          path: str(b.path),
          w: size(b.w),
          h: size(b.h),
        }))
      : [];
    return {
      v: 1,
      token: str(parsed.token),
      title: str(parsed.title),
      rights: parsed.rights === 'comment' ? 'comment' : 'view',
      expiresAt: typeof parsed.expiresAt === 'string' ? parsed.expiresAt : null,
      view: isRecord(parsed.view) ? parsed.view : null,
      boards: boards.filter((b) => b.path !== ''),
      filesBase: str(parsed.filesBase, './f/'),
      commentsUrl: str(parsed.commentsUrl),
    };
  } catch {
    return null;
  }
};

/** The address of a board's file: each segment of its path encoded, a query kept as it is. */
export const boardUrl = (base: string, path: string): string => {
  const at = path.indexOf('?');
  const file = at < 0 ? path : path.slice(0, at);
  const query = at < 0 ? '' : path.slice(at);
  return base + file.split('/').map(encodeURIComponent).join('/') + query;
};

/** The view the owner shared as the view memory takes it: what is not valid stays at its default. */
export const sharedView = (view: IGuestConfig['view']): IViewMemory | undefined => {
  if (view === null) return undefined;
  const width = size(view.width);
  const height = size(view.height);
  const free = view.fit === null;
  return parseView({
    v: 1,
    device: view.device,
    size: width !== null && height !== null ? { w: width, h: height } : null,
    rot: view.rotated,
    chrome: view.chrome,
    browser: view.browser,
    fit: view.fit,
    zoom: free ? { s: view.zoom, x: view.panX, y: view.panY } : undefined,
    locked: view.locked,
    box: null,
    page: view.page,
  });
};
