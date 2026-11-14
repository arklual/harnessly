// Links between the pages of a design. A board's link to another board of its project — a plain
// `<a href="./Оплата.dc.html?variant=Mobile#pay">` or `sinopia:page/Оплата?variant=Mobile#pay` — is not followed by the
// frame: the library's script in the board (internal/design/texts/client.js) tells the host `tk:go`, and the host
// switches its own page (the list, the address, the settings, the view, the agent's context) and hands the link's props
// and block to the board it opens (`tk:go-take`). This file reads the message and finds the page; no React here.
// What a board says is foreign data: every field is checked and clipped.

/** A link a board asked the host to follow. */
export interface IPageGo {
  /** The target as the link names it: the decoded path of a file on the board's server (`file`), else a title or a file name. */
  ref: string;
  file: boolean;
  /** The decoded path of the board that has the link, on its server (a file link is relative to it). */
  from: string;
  /** The target's props as the link wrote them; the board turns the words into its props' values. */
  props: Readonly<Record<string, string>>;
  /** The block to scroll to: an id or a `data-block` name; '' for none. */
  hash: string;
  /** ⌘/Ctrl/Shift + click or target=_blank: a tab of its own. */
  tab: boolean;
}

const MAX_PROPS = 40;
const MAX_NAME = 80;
const MAX_TEXT = 2000;
const clip = (v: unknown, max: number) => (typeof v === 'string' ? v.slice(0, max) : '');

export const readPageGo = (data: unknown): IPageGo | null => {
  if (!data || typeof data !== 'object') return null;
  const m = data as Record<string, unknown>;
  if (m.type !== 'tk:go' || typeof m.ref !== 'string' || m.ref.trim() === '') return null;
  const props: Record<string, string> = {};
  if (m.props && typeof m.props === 'object' && !Array.isArray(m.props)) {
    for (const [name, value] of Object.entries(m.props).slice(0, MAX_PROPS)) {
      if (name !== '' && name.length <= MAX_NAME && typeof value === 'string')
        props[name] = value.slice(0, MAX_TEXT);
    }
  }
  return {
    ref: m.ref.trim().slice(0, 600),
    file: m.file === true,
    from: clip(m.from, 600),
    props,
    hash: clip(m.hash, 200),
    tab: m.tab === true,
  };
};

const BOARD_EXT = /\.dc\.html$/i;
const bare = (name: string) => name.replace(BOARD_EXT, '').trim().toLowerCase();

/** The project's root on the board's server: the board's own path without its path in the project. */
const rootOf = (from: string, current: string | null) => {
  if (current && from.endsWith(`/${current}`)) return from.slice(0, from.length - current.length);
  return from.slice(0, from.lastIndexOf('/') + 1);
};

/**
 * The page a link names, among the pages the host shows (a guest's are those shared with them): a file link by its
 * path from the project's root, else by its file name; a named link by its path, its file name or its title, the
 * `.dc.html` and the case aside. Null: no such page here.
 */
export const pageOfLink = (
  go: Pick<IPageGo, 'ref' | 'file' | 'from'>,
  pages: readonly { path: string; name: string }[],
  current: string | null,
): string | null => {
  if (go.file) {
    const root = rootOf(go.from, current);
    const path = go.ref.startsWith(root) ? go.ref.slice(root.length) : null;
    const exact = path === null ? undefined : pages.find((p) => p.path === path);
    if (exact) return exact.path;
    const file = go.ref.slice(go.ref.lastIndexOf('/') + 1);
    const same = pages.filter((p) => p.path.slice(p.path.lastIndexOf('/') + 1) === file);
    return same.length === 1 ? (same[0]?.path ?? null) : null;
  }
  const wanted = bare(go.ref);
  const byPath = pages.find(
    (p) => bare(p.path) === wanted || bare(p.path.slice(p.path.lastIndexOf('/') + 1)) === wanted,
  );
  if (byPath) return byPath.path;
  return pages.find((p) => p.name.trim().toLowerCase() === wanted)?.path ?? null;
};

/** What the host hands the board it opened for a link: the props the link wrote and its block. */
export const goTake = (go: Pick<IPageGo, 'props' | 'hash'>) => ({
  type: 'tk:go-take' as const,
  props: { ...go.props },
  hash: go.hash,
});

/** A link's props and block wait for the board of `path` to load. */
export interface IPendingTake {
  path: string;
  props: Readonly<Record<string, string>>;
  hash: string;
}

export const hasTake = (go: Pick<IPageGo, 'props' | 'hash'>) =>
  go.hash !== '' || Object.keys(go.props).length > 0;

/** Up to this width a page's `$preview` is a phone's: a link to it shows it in a phone. */
export const PHONE_FRAME_MAX = 600;
export const FRAME_VIEW = { phone: 'phone', wide: 'wide', any: 'any' } as const;
export type TFrameView = (typeof FRAME_VIEW)[keyof typeof FRAME_VIEW];

/** What a page's frame asks of the view: a phone, a wide screen, or nothing (it fills the window). */
export const frameView = (w: number): TFrameView => {
  if (w <= 0) return FRAME_VIEW.any;
  return w <= PHONE_FRAME_MAX ? FRAME_VIEW.phone : FRAME_VIEW.wide;
};
