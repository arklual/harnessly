import type { IPushMessage } from '@/lib/push-message';

/** What the service worker (public/sw.js) says to a page, and where it keeps a click for a page that was asleep. */
export const SW_PUSH_MESSAGE = 'hl:push-message';
export const SW_OPEN_CONVERSATION = 'hl:open-conversation';
export const SW_CLEAR_NOTIFICATIONS = 'clear-notifications';
export const PUSH_NAV_CACHE = 'push-nav';

/** The cache key the worker writes under; it derives it from its own directory, which is the app's base path. */
export const pushNavKey = (basePath: string): string => `${basePath.replace(/\/+$/, '')}/_push-pending`;

export interface IPushNavigation {
  tabId: string;
  workspaceId: string | null;
}

/** A click payload read back from the cache or a worker message; anything without a tab to open is dropped. */
export const parsePushNavigation = (raw: unknown): IPushNavigation | null => {
  if (!raw || typeof raw !== 'object') return null;
  const { tabId, workspaceId } = raw as Record<string, unknown>;
  if (typeof tabId !== 'string' || !tabId) return null;
  return { tabId, workspaceId: typeof workspaceId === 'string' && workspaceId ? workspaceId : null };
};

export type TWorkerMessage =
  { kind: 'push'; message: IPushMessage } | { kind: 'open'; navigation: IPushNavigation };

/** Sorts a message from the worker into what the page does with it; foreign messages are ignored. */
export const readWorkerMessage = (data: unknown): TWorkerMessage | null => {
  if (!data || typeof data !== 'object') return null;
  const body = data as Record<string, unknown>;
  if (body.type === SW_PUSH_MESSAGE) {
    const message = body.message;
    if (message && typeof message === 'object' && typeof (message as { kind?: unknown }).kind === 'string')
      return { kind: 'push', message: message as IPushMessage };
    return null;
  }
  if (body.type === SW_OPEN_CONVERSATION) {
    const navigation = parsePushNavigation(body);
    return navigation ? { kind: 'open', navigation } : null;
  }
  return null;
};
