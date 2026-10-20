import { appConfig } from '@/platform/config';
import { boardsOrigin, isArtifactsUrl, isBoardsUrl } from '@/lib/site';

/** `/a/<slug>` is a document of the documents service, `/b/<slug>` a board of the boards service; `/b/api` is not a board. */
const DOC_PATH = /^\/a\/([\w-]+)\/?$/;
const BOARD_SLUG_PATH = /^\/b\/(?!api\/?$)([\w-]+)\/?$/;
const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
/** The page's own document route: `<base>/docs/<slug>`. */
const readerPath = () => new RegExp(`^${escape(appConfig().basePath)}/docs/([\\w-]+)/?$`);
const paneDocPath = () => new RegExp(`^${escape(appConfig().basePath)}/docs/[\\w-]+$`);
/** Where document links open: this page (its reader route) or the reader the documents service redirects to. */
const readerHosts = (): string[] => {
  const hosts = typeof window === 'undefined' ? [] : [window.location.hostname];
  const reader = appConfig().readerUrl;
  if (reader) {
    try {
      hosts.push(new URL(reader.replaceAll('{slug}', 'x').replaceAll('{version}', '')).hostname);
    } catch {
      // a malformed reader template names no host
    }
  }
  return hosts;
};

/** The slug of a document or a board, from its portal address or from the reader's address on the platform. */
export const docSlugOf = (url: string): string | null => {
  try {
    const parsed = new URL(url);
    const { protocol, hostname, pathname } = parsed;
    if (protocol !== 'https:' && protocol !== 'http:') return null;
    if (isArtifactsUrl(parsed) && DOC_PATH.test(pathname)) return pathname.match(DOC_PATH)?.[1] ?? null;
    if (isBoardsUrl(parsed) && BOARD_SLUG_PATH.test(pathname))
      return pathname.match(BOARD_SLUG_PATH)?.[1] ?? null;
    return protocol === 'https:' && readerHosts().includes(hostname)
      ? (pathname.match(readerPath())?.[1] ?? null)
      : null;
  } catch {
    return null;
  }
};

/** A published document or board opens in the right panel through the app's own route: the portal address redirects through the login page, which refuses frames. */
export const docPathOf = (url: string): string | null => {
  const slug = docSlugOf(url);
  return slug ? `${appConfig().basePath}/docs/${slug}` : null;
};

/** The reader's address on the platform, as the publishing tool prints it (`viewUrl`). */
export const isDocReaderUrl = (url: string): boolean => {
  try {
    return readerHosts().includes(new URL(url).hostname) && docSlugOf(url) !== null;
  } catch {
    return false;
  }
};

/** What a document tab may frame: only the app's own reader, whatever a stored or requested target says. */
export const isPaneDocPath = (path: string): boolean => paneDocPath().test(path);

const BOARD_PATH = /^\/b\/(?!api\/?$)([\w-]+)\/?$/;
const BOARD_KEY = /^[\w-]{1,64}$/;

/**
 * The address a board tab frames: the board itself with its key and, for a snapshot, its `at`; nothing else of the link. A board
 * opens to whoever holds its key and allows this platform to frame it; the reader knows only boards bound to a
 * conversation, so a link that carries the key does not go through the reader.
 */
export const boardFrameUrlOf = (url: string): string | null => {
  try {
    const parsed = new URL(url);
    const { pathname, searchParams } = parsed;
    const origin = boardsOrigin();
    if (!origin || !isBoardsUrl(parsed)) return null;
    const slug = pathname.match(BOARD_PATH)?.[1];
    const key = searchParams.get('k');
    if (!slug || !key || !BOARD_KEY.test(key)) return null;
    // `at` is a snapshot of the board, read-only (boards 0.12.0): without it the board as it is now would open in
    // its place. An id the board does not know shows the current scene there too, so a malformed one is dropped.
    const at = searchParams.get('at');
    return `${origin}/b/${slug}?k=${key}${at && BOARD_KEY.test(at) ? `&at=${at}` : ''}`;
  } catch {
    return null;
  }
};

/** What a link to a document or a board opens as in the panel: the board itself when the link carries its key, the reader otherwise. */
export const paneDocTargetOf = (url: string): string | null => boardFrameUrlOf(url) ?? docPathOf(url);

/** A target a document tab accepts, wherever it came from: the reader's path or a board's own address in its one canonical form. */
export const isPaneDocTarget = (target: string): boolean =>
  isPaneDocPath(target) || boardFrameUrlOf(target) === target;
