import { artifactsOrigin, isArtifactsUrl, isBoardsUrl, isTrackerHost } from '@/lib/site';
import { isGitlabHost, type TGitlabHost } from '@/lib/gitlab-hosts';
import { docSlugOf, isDocReaderUrl } from '@/lib/hl-doc-path';
import { t } from '@/lib/i18n';
export interface IIssueMeta {
  kind: 'issue';
  key: string;
  title: string;
  state: string | null;
  resolved: boolean;
  assignee: { name: string; avatar: string | null } | null;
  /** Epic, Idea, Feature, Bug… */
  type?: string | null;
  /** The epic (or idea) this issue is a subtask of. */
  parent?: { key: string; title: string } | null;
}

export type TMrState = 'opened' | 'merged' | 'closed' | 'locked';

export interface IMrMeta {
  kind: 'mr';
  iid: number;
  title: string;
  state: TMrState;
  project: string;
  draft: boolean;
}

export interface IArtifactMeta {
  kind: 'artifact';
  title: string;
  description: string | null;
}

export type TLinkMeta = IIssueMeta | IMrMeta | IArtifactMeta;

export type TIssueTone = 'done' | 'progress' | 'review' | 'open';

// Tracker state names as the tracker spells them, in English or Russian: data, not page text.
const REVIEW_STATES = /test|review|verif|провер|ревью|тест/i;
const PROGRESS_STATES = /progress|work|develop|работ/i;

export const issueTone = (state: string | null, resolved: boolean): TIssueTone => {
  if (resolved) return 'done';
  if (!state) return 'open';
  if (REVIEW_STATES.test(state)) return 'review';
  if (PROGRESS_STATES.test(state)) return 'progress';
  return 'open';
};

export const MR_STATE_LABEL: Record<TMrState, string> = {
  opened: t('lib.linkMeta.mrOpened'),
  merged: t('lib.linkMeta.mrMerged'),
  closed: t('lib.linkMeta.mrClosed'),
  locked: t('lib.linkMeta.mrClosed'),
};

export const ISSUE_TONE_LABEL: Record<TIssueTone, string> = {
  done: t('lib.linkMeta.issueDone'),
  progress: t('lib.linkMeta.issueProgress'),
  review: t('lib.linkMeta.issueReview'),
  open: t('lib.linkMeta.issueOpen'),
};

const ARTIFACT_URL = /^\/(?:a|b)\/(?!api\/?$)[\w-]{1,160}\/?$/;
const ISSUE_URL = /^\/(?:youtrack\/)?issue\/([A-Z][A-Z0-9]{0,15}-\d{1,7})(?:\/[^?#]*)?$/;
const MR_URL = /^\/((?:[\w.-]{1,100}\/){1,6}[\w.-]{1,100})\/-\/merge_requests\/(\d{1,9})(?:\/[\w-]*)?$/;

export type TLinkTarget =
  | { kind: 'issue'; key: string }
  | { kind: 'mr'; host: TGitlabHost; project: string; iid: number }
  | { kind: 'artifact'; url: string };

/**
 * Strict allowlist of URLs the server may fetch metadata for; everything else is rejected. The tracker and the GitLab
 * hosts are plain https addresses. The documents and boards services are recognised by origin, the scheme and the port
 * included, so the compose workspace (http://localhost with a port per service) gets titles as well as a host each.
 */
export const parseLinkTarget = (raw: string): TLinkTarget | null => {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.username || url.password) return null;
  const plainHttps = url.protocol === 'https:' && !url.port;
  if (plainHttps && isTrackerHost(url.hostname)) {
    const key = url.pathname.match(ISSUE_URL)?.[1];
    return key ? { kind: 'issue', key } : null;
  }
  if (plainHttps && isGitlabHost(url.hostname)) {
    const mr = url.pathname.match(MR_URL);
    if (!mr || mr[1].split('/').some((part) => part === '..' || part === '.')) return null;
    return { kind: 'mr', host: url.hostname, project: mr[1], iid: Number(mr[2]) };
  }
  // The reader's address on the platform names the same document as its portal address; a board read this way has
  // no page under /a/ and simply gets no title.
  const docs = artifactsOrigin();
  if (docs && plainHttps && isDocReaderUrl(url.href))
    return { kind: 'artifact', url: `${docs}/a/${docSlugOf(url.href)}` };
  if ((isArtifactsUrl(url) || isBoardsUrl(url)) && ARTIFACT_URL.test(url.pathname)) {
    const k = url.searchParams.get('k');
    if (k !== null && !/^[\w-]{1,64}$/.test(k)) return null;
    return { kind: 'artifact', url: `${url.origin}${url.pathname}${k ? `?k=${k}` : ''}` };
  }
  return null;
};

/** Cache key that ignores fragments and irrelevant query parameters. */
export const linkTargetKey = (target: TLinkTarget) =>
  target.kind === 'issue'
    ? `issue:${target.key}`
    : target.kind === 'mr'
      ? `mr:${target.host}/${target.project}!${target.iid}`
      : `artifact:${target.url}`;
