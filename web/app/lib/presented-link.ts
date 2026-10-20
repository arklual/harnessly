import {
  githubKeyOf,
  githubRefOf,
  gitlabHosts,
  isArtifactsUrl,
  isBoardsUrl,
  isTrackerHost,
} from '@/lib/site';
import { appConfig } from '@/platform/config';
import { isDocReaderUrl } from '@/lib/hl-doc-path';
import { t } from '@/lib/i18n';

export type LinkKind =
  'artifact' | 'claude-artifact' | 'mr' | 'issue' | 'gitlab' | 'github' | 'web' | 'file' | 'mail';

export interface IPresentedLink {
  kind: LinkKind;
  label: string;
  detail: string;
  key?: string;
  iid?: number;
  project?: string;
  /** A board of the boards service, shown among the documents. */
  board?: boolean;
}

const ISSUE_PATH = /^\/(?:youtrack\/)?issue\/([A-Z][A-Z0-9]*-\d+)(?:\/|$)/;
const MR_PATH = /^\/((?:[\w.-]+\/)+[\w.-]+)\/-\/merge_requests\/(\d+)(?:\/|$)/;
const ARTIFACT_PATH = /^\/a\/[\w-]+\/?$/;
/** A board of the boards service; `api` is its documentation, not a board. */
const BOARD_PATH = /^\/b\/(?!api\/?$)[\w-]+\/?$/;
const CLAUDE_ARTIFACT_PATH = /^\/(?:public\/artifacts|artifact|code\/artifact)\/[\w-]+\/?$/;

/** Presentation never exposes query tokens; the anchor retains its original destination. */
export const presentLink = (href: string): IPresentedLink => {
  if (href.startsWith('mailto:')) {
    const address = href.slice(7).split('?')[0];
    return { kind: 'mail', label: address, detail: address };
  }
  let url: URL;
  try {
    url = new URL(href);
    if (!['https:', 'http:'].includes(url.protocol)) throw new Error('local');
  } catch {
    const path = href.split(/[?#]/)[0];
    return { kind: 'file', label: path.split('/').filter(Boolean).at(-1) || path, detail: path };
  }
  const host = url.hostname.replace(/^www\./, '');
  const detail = host + (url.pathname === '/' ? '' : url.pathname);

  if (isTrackerHost(host)) {
    const key = url.pathname.match(ISSUE_PATH)?.[1];
    return key ? { kind: 'issue', label: key, detail, key } : { kind: 'web', label: detail, detail };
  }
  if (gitlabHosts().includes(host)) {
    const mr = url.pathname.match(MR_PATH);
    if (mr) {
      const project = mr[1];
      const iid = Number(mr[2]);
      return { kind: 'mr', label: `${project.split('/').at(-1)} !${iid}`, detail, iid, project };
    }
    return { kind: 'gitlab', label: detail, detail };
  }
  if (isBoardsUrl(url) && BOARD_PATH.test(url.pathname))
    return { kind: 'artifact', label: t('lib.presentedLink.board'), detail, board: true };
  if (isArtifactsUrl(url))
    return {
      kind: ARTIFACT_PATH.test(url.pathname) ? 'artifact' : 'web',
      label: t('lib.presentedLink.document'),
      detail,
    };
  // The address the publishing tool gives people is the reader on the platform, not the portal.
  if (isDocReaderUrl(href)) return { kind: 'artifact', label: t('lib.presentedLink.document'), detail };
  if (host === 'claude.ai' && CLAUDE_ARTIFACT_PATH.test(url.pathname))
    return { kind: 'claude-artifact', label: t('lib.presentedLink.claudeArtifact'), detail };
  const github = githubRefOf(href);
  if (github?.pull)
    return {
      kind: 'mr',
      label: `${github.repo} #${github.number}`,
      detail,
      iid: github.number,
      project: `${github.owner}/${github.repo}`,
    };
  if (github) return { kind: 'issue', label: githubKeyOf(github), detail, key: githubKeyOf(github) };
  if (host === 'github.com' || host === appConfig().githubHost)
    return { kind: 'github', label: detail, detail };
  return { kind: 'web', label: detail, detail };
};

/** Links that are work products rather than references: artifact pages, merge requests and tracker issues. */
export const isWorkProduct = (kind: LinkKind) =>
  kind === 'artifact' || kind === 'claude-artifact' || kind === 'mr' || kind === 'issue';

export const hasLinkMeta = (kind: LinkKind) => kind === 'artifact' || kind === 'mr' || kind === 'issue';
