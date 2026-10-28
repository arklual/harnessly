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

/** What the owner published of a deck (the runtime's index, passed through by the gateway). */
export interface IGuestDeck {
  /** The recording with its chapters (`page` is a board's id, `t` ms); null where there is none. */
  video: { chapters: { page: string; title: string; t: number }[] } | null;
  /** A PDF is published. */
  pdf: boolean;
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
  /** What the project is: a deck opens as slides. Null from a gateway or runtime that does not say. */
  kind: 'site' | 'slides' | 'ds' | null;
  /** The deck's published recording and PDF; null for anything but a deck, and for a link that shows a snapshot or some pages. */
  deck: IGuestDeck | null;
  /** Where the project's files are, with the trailing slash. */
  filesBase: string;
  commentsUrl: string;
  /** Where the published recording and PDF are, with the trailing slash (`video.mp4`, `deck.pdf`). */
  mediaBase: string;
  /** Where the views of a board are reported. */
  seenUrl: string;
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

const str = (v: unknown, fallback = ''): string => (typeof v === 'string' ? v : fallback);
const size = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null);

const KINDS: readonly string[] = ['site', 'slides', 'ds'];

const readDeck = (v: unknown): IGuestDeck | null => {
  if (!isRecord(v)) return null;
  let video: IGuestDeck['video'] = null;
  if (isRecord(v.video)) {
    const list = Array.isArray(v.video.chapters) ? v.video.chapters : [];
    video = {
      chapters: list.filter(isRecord).flatMap((c) => {
        const at = size(c.t);
        return typeof c.page === 'string' && c.page !== '' && at !== null && at >= 0
          ? [{ page: c.page, title: str(c.title), t: at }]
          : [];
      }),
    };
  }
  return { video, pdf: v.pdf === true };
};

/** The sibling of the files' address that holds the published media (`/s/<token>/f/` → `/s/<token>/m/`). */
export const mediaBaseOf = (filesBase: string): string =>
  filesBase.endsWith('/f/') ? `${filesBase.slice(0, -2)}m/` : './m/';

/** The address views are reported to, beside the comments' (`…/comments` → `…/seen`). */
export const seenUrlOf = (commentsUrl: string): string =>
  commentsUrl.endsWith('/comments') ? `${commentsUrl.slice(0, -'comments'.length)}seen` : './seen';

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
    const filesBase = str(parsed.filesBase, './f/');
    const commentsUrl = str(parsed.commentsUrl);
    return {
      v: 1,
      token: str(parsed.token),
      title: str(parsed.title),
      rights: parsed.rights === 'comment' ? 'comment' : 'view',
      expiresAt: typeof parsed.expiresAt === 'string' ? parsed.expiresAt : null,
      view: isRecord(parsed.view) ? parsed.view : null,
      boards: boards.filter((b) => b.path !== ''),
      kind: KINDS.includes(str(parsed.kind)) ? (parsed.kind as IGuestConfig['kind']) : null,
      deck: readDeck(parsed.deck),
      filesBase,
      commentsUrl: str(parsed.commentsUrl),
      mediaBase: mediaBaseOf(filesBase),
      seenUrl: seenUrlOf(commentsUrl),
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
