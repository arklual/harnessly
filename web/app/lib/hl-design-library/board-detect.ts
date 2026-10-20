import { locale } from '@/lib/i18n';
import type { ILibraryBoard } from '@/lib/hl-design-library/types';

const DC_SUFFIX = '.dc.html';

/**
 * A board of a Claude Design project can be a thin wrapper around a real page: its `renderVals()` builds the address
 * of `lib/page.html?id=…&v=…` and an iframe shows it. The right panel's element picker works in one frame and does not
 * follow into the nested one, so such a board is listed as the page itself (dark theme, the board's default variant).
 */
export const innerPageBoard = (title: string, source: string): ILibraryBoard | null => {
  const id = source.match(/lib\/page\.html\?id=([\w-]+)&v='/)?.[1];
  if (!id) return null;
  const variant = source.match(/props\.variant\s*\?\?\s*'([\w-]+)'/)?.[1] ?? 'Desktop';
  const mobile = /&m=1\b/.test(source);
  return {
    title,
    path: `lib/page.html?id=${id}&v=${variant}&theme=dark${mobile ? '&m=1' : ''}`,
    w: null,
    h: null,
  };
};

export const boardTitleOf = (file: string): string => file.replace(/\.dc\.html$/, '');

export const isBoardFile = (file: string): boolean => file.endsWith(DC_SUFFIX) && !file.includes('/');

/**
 * What a board is called in a claim: the file name without `.dc.html`, which a rename of its title does not
 * change. A board shown as the page of its wrapper (`lib/page.html?…`) is named by its title: the wrapper is `<title>.dc.html`.
 */
export const boardKeyOf = (board: Pick<ILibraryBoard, 'title' | 'path'>): string =>
  isBoardFile(board.path) ? boardTitleOf(board.path) : board.title;

/** The overview board's title as Claude Design projects name it: data compared with board titles, not interface text. */
const FIRST = 'Все страницы';

/** Wrapper boards become their pages, the rest stay as they are; the overview board goes first, the rest by name. */
export const detectBoards = (sources: ReadonlyMap<string, string>): ILibraryBoard[] => {
  const boards: ILibraryBoard[] = [];
  for (const [file, source] of sources) {
    const title = boardTitleOf(file);
    boards.push(innerPageBoard(title, source) ?? { title, path: file, w: null, h: null });
  }
  return boards.sort(
    (a, b) =>
      Number(b.title === FIRST) - Number(a.title === FIRST) ||
      a.title.localeCompare(b.title, locale(), { numeric: true }),
  );
};

export interface IBoardTweaks {
  /** Screen states the board offers (`Desktop`, `Empty`, `Loading`, `ErrorState`…), in its own order. */
  variants: string[];
  /** The board switches between the dark and the light theme. */
  themes: boolean;
}

const ENTITIES: Record<string, string> = {
  '&quot;': '"',
  '&#39;': "'",
  '&apos;': "'",
  '&lt;': '<',
  '&gt;': '>',
  '&amp;': '&',
};
const unescapeAttribute = (value: string): string =>
  value.replace(/&(?:quot|#39|apos|lt|gt|amp);/g, (entity) => ENTITIES[entity]);

/**
 * The tweaks a board declares in `data-props` of its script, as Claude Design's Tweaks panel shows them: the screen
 * variant and the theme. A wrapper board's page takes them as `v=` and `theme=` of its address.
 */
export const parseBoardTweaks = (source: string): IBoardTweaks | null => {
  const attribute = source.match(/data-props=(?:"([^"]*)"|'([^']*)')/);
  if (!attribute) return null;
  let props: unknown;
  try {
    props = JSON.parse(unescapeAttribute(attribute[1] ?? attribute[2] ?? ''));
  } catch {
    return null;
  }
  if (typeof props !== 'object' || props === null) return null;
  const { variant, theme } = props as { variant?: { options?: unknown }; theme?: unknown };
  const variants = Array.isArray(variant?.options)
    ? variant.options
        .filter((item): item is string => typeof item === 'string' && /^[\w-]{1,40}$/.test(item))
        .slice(0, 24)
    : [];
  const themes = typeof theme === 'object' && theme !== null;
  return variants.length > 0 || themes ? { variants, themes } : null;
};
