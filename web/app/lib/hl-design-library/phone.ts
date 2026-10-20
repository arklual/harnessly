/**
 * "Send to phone": a board of the design library goes to the person's phone as a push, and a tap on it
 * opens the app on that board. The request travels as query parameters of the push's `url`, so the same link works from
 * a cold start (the service worker opens the window) and from a window that is already open. Nothing here imports node modules.
 */
import { plural, t } from '@/lib/i18n';
import { APP_BASE_PATH } from '@/lib/app-path';
import { LIBRARY_LIMITS, parseLibraryPagePath } from '@/lib/hl-design-library/types';

/** A window event for a board request that arrived while the app is open (the toast of a push). */
export const OPEN_BOARD_EVENT = 'hl:open-design-board' as const;

const QUERY = { tab: 'open', address: 'board', title: 'boardTitle' } as const;

const TAB_ID_RE = /^[A-Za-z0-9_-]{1,64}$/;
const BOARD_ADDRESS_RE = /^localhost:\d{1,5}(\/a\/.+)$/;
const ADDRESS_MAX = 1200;

export const NO_PHONE_ERROR = t('lib.designLibraryPhone.noPhone');

export interface IBoardOpenRequest {
  /** The conversation whose right panel shows the board: the one the person sent it from. */
  tabId: string;
  /** What the panel's address field takes: `localhost:<port>/a/<artifact>/p/<board>`. */
  address: string;
  title: string;
}

export const isBoardTabId = (value: string): boolean => TAB_ID_RE.test(value);

/** The board a page of the library server shows, as the path the manifest records: decoded, with its query. */
export const parseLibraryBoardPath = (pagePath: string): { artifact: string; board: string } | null => {
  const parsed = parseLibraryPagePath(pagePath);
  if (!parsed) return null;
  const at = pagePath.search(/[?#]/);
  const query = at < 0 || pagePath[at] === '#' ? '' : pagePath.slice(at).split('#', 1)[0];
  return { artifact: parsed.ref, board: `${parsed.file}${query}` };
};

/** The port a board address names: `localhost:<port>/a/…`. */
export const boardAddressPort = (address: string): number | null => {
  const match = /^localhost:(\d{1,5})\//.exec(address);
  return match ? Number(match[1]) : null;
};

const validAddress = (address: string): boolean => {
  if (address.length > ADDRESS_MAX) return false;
  const match = BOARD_ADDRESS_RE.exec(address);
  return match !== null && parseLibraryPagePath(match[1]) !== null;
};

/** Where a tap on the push leads: the app root with the request in the query (the workspace page reads and clears it). */
export const boardOpenUrl = ({ tabId, address, title }: IBoardOpenRequest): string =>
  `${APP_BASE_PATH}/?${new URLSearchParams({ [QUERY.tab]: tabId, [QUERY.address]: address, [QUERY.title]: title })}`;

type TQuery = Record<string, string | string[] | undefined>;

/** The request a link carries, or null for anything that is not a well-formed board of the library on this machine. */
export const parseBoardOpenQuery = (query: TQuery): IBoardOpenRequest | null => {
  const tabId = query[QUERY.tab];
  const address = query[QUERY.address];
  const title = query[QUERY.title];
  if (
    typeof tabId !== 'string' ||
    typeof address !== 'string' ||
    !isBoardTabId(tabId) ||
    !validAddress(address)
  )
    return null;
  return { tabId, address, title: typeof title === 'string' ? title.slice(0, LIBRARY_LIMITS.title) : '' };
};

export const parseBoardOpenUrl = (url: string | undefined): IBoardOpenRequest | null => {
  if (!url) return null;
  try {
    return parseBoardOpenQuery(Object.fromEntries(new URL(url, 'https://hl.invalid').searchParams));
  } catch {
    return null;
  }
};

export const withoutBoardOpenQuery = (query: TQuery): TQuery => {
  const { [QUERY.tab]: _tab, [QUERY.address]: _address, [QUERY.title]: _title, ...rest } = query;
  return rest;
};

export const sentLabel = (n: number): string => plural(n, 'lib.designLibraryPhone.sent');
