import { locale, plural, t } from '@/lib/i18n';
import { boardKeyOf } from '@/lib/hl-design-library/board-detect';
import type { TCheckStatus } from '@/types/server/check';
import { isAgentRunning } from '@/lib/hl-design-library/group';
import type {
  IArtifactAgent,
  ILibraryArtifact,
  ILibraryBoard,
  ILibraryClaim,
} from '@/lib/hl-design-library/types';

/**
 * The library as a section of its own: what the project grid, the project page and the board page show,
 * worked out from the library list. Pure, shared by the components and the tests.
 */
export const BOARD_STATUS_LABEL = {
  ok: t('lib.designLibraryHomeModel.statusOk'),
  warning: t('lib.designLibraryHomeModel.statusWarning'),
  error: t('lib.designLibraryHomeModel.statusError'),
  blank: t('lib.designLibraryHomeModel.statusBlank'),
} satisfies Record<TCheckStatus, string>;

/** Chats at work on a project right now: busy, or waiting for the person. */
export const workingNow = (agents: readonly IArtifactAgent[]): IArtifactAgent[] =>
  agents.filter((agent) => agent.state === 'busy' || agent.state === 'needs-input');

export const NEW_CHAT = 'new' as const;

/** The chat a comment goes to when the person did not choose: the running one that was active last, else a new chat. */
export const defaultTarget = (agents: readonly IArtifactAgent[]): string => {
  const running = agents.filter((agent) => isAgentRunning(agent.state));
  const latest = [...running].sort((a, b) => (b.lastEventAt ?? 0) - (a.lastEventAt ?? 0))[0];
  return latest ? latest.tabId : NEW_CHAT;
};

/** A chosen chat that is gone from the list (closed, or never worked on this project) falls back to the default. */
export const resolveTarget = (chosen: string | null, agents: readonly IArtifactAgent[]): string =>
  chosen === NEW_CHAT || (chosen !== null && agents.some((agent) => agent.tabId === chosen))
    ? (chosen as string)
    : defaultTarget(agents);

const MINUTE_MS = 60_000;
const DAY_MINUTES = 24 * 60;
const RECENT_DAYS = 30;

/** "just now", "5 min ago", "2 h ago", "3 d ago", then the date: the way the gallery says when a project last changed. */
export const relativeWhen = (ms: number, now: number = Date.now()): string => {
  const minutes = Math.floor(Math.max(0, now - ms) / MINUTE_MS);
  if (minutes < 1) return t('lib.designLibraryHomeModel.justNow');
  if (minutes < 60) return t('lib.designLibraryHomeModel.minutesAgo', { minutes });
  if (minutes < DAY_MINUTES)
    return t('lib.designLibraryHomeModel.hoursAgo', { hours: Math.floor(minutes / 60) });
  const days = Math.floor(minutes / DAY_MINUTES);
  if (days < RECENT_DAYS) return t('lib.designLibraryHomeModel.daysAgo', { days });
  const sameYear = new Date(ms).getFullYear() === new Date(now).getFullYear();
  return new Intl.DateTimeFormat(locale(), {
    day: 'numeric',
    month: 'long',
    ...(sameYear ? {} : { year: 'numeric' }),
  }).format(ms);
};

/** "Alex is editing "Payouts"", and "and 2 more" when more boards are being edited; the one touched last comes first. */
export const claimsLine = (claims: readonly ILibraryClaim[]): string => {
  const [first, ...rest] = [...claims].sort((a, b) => b.since - a.since);
  if (!first) return '';
  const head = t('lib.designLibraryHomeModel.personEditing', { person: first.person, board: first.board });
  return rest.length > 0 ? t('lib.designLibraryHomeModel.andMore', { head, count: rest.length }) : head;
};

/** The claim on a board, if some chat holds it or is seen editing it. */
export const claimOfBoard = (
  claims: readonly ILibraryClaim[],
  board: Pick<ILibraryBoard, 'title' | 'path'>,
): ILibraryClaim | null => claims.find((claim) => claim.board === boardKeyOf(board)) ?? null;

/** The muted line under a board's cover when a chat is at it: who, and in which chat. */
export const boardClaimCaption = (claim: ILibraryClaim | null): string =>
  claim ? t('lib.designLibraryHomeModel.boardClaim', { person: claim.person, chat: claim.chatTitle }) : '';

/** The muted line under a project's title: when it changed, and who is at it now (else that somebody is). */
export const projectCaption = (
  project: Pick<ILibraryArtifact, 'updatedAt' | 'agents' | 'claims'>,
  now: number = Date.now(),
): string => {
  const changed =
    project.updatedAt > 0
      ? t('lib.designLibraryHomeModel.changed', { when: relativeWhen(project.updatedAt, now) })
      : '';
  const working =
    claimsLine(project.claims) ||
    (workingNow(project.agents).length > 0 ? t('lib.designLibraryHomeModel.workInProgress') : '');
  return [changed, working].filter(Boolean).join(' · ');
};

export const boardsLabel = (n: number): string => plural(n, 'lib.designLibraryHomeModel.boards');

/** Two letters for a cover that has no picture. */
export const titleInitials = (title: string): string =>
  title
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0])
    .join('')
    .toUpperCase() || 'CD';
