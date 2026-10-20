import { presentLink } from '@/lib/presented-link';
import type { IArtifactLink } from '@/lib/hl-artifacts';
import { docPathOf, paneDocTargetOf } from '@/lib/hl-doc-path';
import { PANE_SEEN_LIMIT, paneTabId, type IPaneTabRequest } from '@/lib/hl-pane-tabs';
import type { IChatStand, IChatUrl } from '@/lib/hl-preview-chat-urls';

/**
 * What the right panel opens by itself: a document or a board the agent has just published and the dev server
 * it has just started. Everything else waits for a click. Pure decisions; the hook around them is `use-pane-auto-open`.
 */

export interface IAutoCandidate {
  /** What `seen` remembers: one object opens by itself once per conversation. */
  id: string;
  request: IPaneTabRequest;
  /** When the conversation last spoke of it. */
  at: number;
  /** A dev server: it opens once this port answers as a site. */
  port?: number;
  /** A feature stand: like a server, it does not replace a page the person has in the browser. */
  stand?: true;
}

/** Older than this is history the person comes back to, not something made a moment ago. */
export const AUTO_FRESH_MS = 30 * 60_000;
/** A reply that lists a dozen documents opens the newest ones only. */
export const AUTO_MAX_AT_ONCE = 3;

export const docCandidates = (links: readonly IArtifactLink[]): IAutoCandidate[] =>
  links.flatMap((link): IAutoCandidate[] => {
    const path = docPathOf(link.url);
    if (!path) return [];
    // The id stays the reader's path, key or no key: one board is one thing to remember, and what was seen before
    // boards got a tab of their own stays seen.
    return [
      {
        id: paneTabId('artifacts', path),
        request: {
          kind: 'artifacts',
          target: paneDocTargetOf(link.url) ?? path,
          title: link.label ?? presentLink(link.url).label,
        },
        at: link.timestamp,
      },
    ];
  });

export const serverCandidateId = (port: number): string => `browser:${port}`;

export const serverCandidates = (urls: readonly IChatUrl[]): IAutoCandidate[] =>
  urls.map((url) => ({
    id: serverCandidateId(url.port),
    request: { kind: 'browser', target: `localhost:${url.port}${url.path === '/' ? '' : url.path}` },
    at: url.at,
    port: url.port,
  }));

export const standCandidates = (stands: readonly IChatStand[]): IAutoCandidate[] =>
  stands.map((stand) => ({
    id: `browser:stand:${stand.host}`,
    request: { kind: 'browser', target: stand.url },
    at: stand.at,
    stand: true,
  }));

export interface IAutoPlan {
  seen: string[];
  /** Oldest first: the last one ends up in front. */
  open: IAutoCandidate[];
}

/**
 * The first look at a conversation only remembers what is already there. Later, what was not there before opens when
 * it is fresh; a stale one is remembered without opening. Whatever opened once never opens by itself again, so a tab
 * the person closed stays closed.
 */
export const planAutoOpen = (
  seen: readonly string[] | undefined,
  candidates: readonly IAutoCandidate[],
  now: number,
): IAutoPlan | null => {
  if (seen === undefined)
    return { seen: candidates.map((candidate) => candidate.id).slice(-PANE_SEEN_LIMIT), open: [] };
  const known = new Set(seen);
  const added = candidates.filter((candidate) => !known.has(candidate.id));
  if (added.length === 0) return null;
  const open = added
    .filter((candidate) => candidate.at >= now - AUTO_FRESH_MS)
    .sort((a, b) => a.at - b.at)
    .slice(-AUTO_MAX_AT_ONCE);
  return { seen: [...seen, ...added.map((candidate) => candidate.id)].slice(-PANE_SEEN_LIMIT), open };
};

type TSettingStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

const SETTING_KEY = 'hl:pane-auto-open';

/** On unless the person switched it off on this device. */
export const readAutoOpenSetting = (storage: TSettingStorage | null): boolean => {
  try {
    return storage?.getItem(SETTING_KEY) !== '0';
  } catch {
    return true;
  }
};

export const writeAutoOpenSetting = (storage: TSettingStorage | null, enabled: boolean): void => {
  try {
    if (enabled) storage?.removeItem(SETTING_KEY);
    else storage?.setItem(SETTING_KEY, '0');
  } catch {}
};
