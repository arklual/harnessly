// Which subchat a tab of the pane came from: what a delegated chat's pane is asked opens in the pane of the
// chat that delegated it, and its tab says whose it is. The mark lives with the intent on the machine (`from`, kept a
// day), and is read again off every look at the chat's intents, so it outlives a reload; a tab is known by what it
// shows (a design, a file, an app, a document, a merge request, a task; a page of the browser is not marked).
import type { IPaneRequest } from '@/next/features/pane/intents';
import type { TTab } from '@/next/features/pane/pane-tabs';

const marks = new Map<string, Map<string, string>>();

const requestKey = (request: IPaneRequest): string | null => {
  switch (request.k) {
    case 'design':
    case 'files':
      return request.target === null ? null : `${request.k}:${request.target}`;
    case 'app':
      return request.target === null ? null : `app:${request.target.split('/')[0]}`;
    case 'legacy':
      return request.target === null ? null : legacyKey(request.kind, request.target);
    default:
      return null;
  }
};

const legacyKey = (kind: string, target: string): string => `legacy:${kind}:${target}`;

const tabKey = (tab: TTab): string | null => {
  switch (tab.kind) {
    case 'designly':
      return tab.design === null ? null : `design:${tab.design}`;
    case 'files':
      return `files:${tab.path}`;
    case 'app':
      return `app:${tab.app}`;
    default:
      return null;
  }
};

/** Marks what a subchat's requests open in the chat's pane; a request of the chat's own clears the mark. */
export const markLifted = (conv: string, request: IPaneRequest, from: string | null): void => {
  const key = requestKey(request);
  if (key === null) return;
  const have = marks.get(conv) ?? new Map<string, string>();
  if (from === null) have.delete(key);
  else have.set(key, from);
  marks.set(conv, have);
};

/** The subchat the tab came from, by its title; null for the chat's own. */
export const liftedFrom = (conv: string, tab: TTab): string | null => {
  const key = tabKey(tab);
  return key === null ? null : (marks.get(conv)?.get(key) ?? null);
};

/** The subchat a tab of Legacy's pane (a document, a merge request, a task) came from; null for the chat's own. */
export const liftedFromLegacy = (conv: string, kind: string, target: string | null): string | null =>
  target === null ? null : (marks.get(conv)?.get(legacyKey(kind, target)) ?? null);

/** Forgets every mark (tests). */
export const resetLifted = (): void => marks.clear();
