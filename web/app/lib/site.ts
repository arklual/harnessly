// Addresses of the services this installation works with, from the page's config (platform/config.ts): the tracker,
// the GitLab hosts, the documents and boards services and this page itself. Nothing here names a real host.
import { appConfig } from '@/platform/config';

const hostOf = (url: string | null): string | null => {
  if (!url) return null;
  try {
    return new URL(url).hostname;
  } catch {
    return null;
  }
};

/** A task's page in the tracker, or null when no tracker is configured. */
export const issueUrlOf = (key: string): string | null => {
  if (GITHUB_KEY_RE.test(key)) return githubIssueUrl(key);
  const template = appConfig().trackerIssueUrl;
  return template ? template.replaceAll('{key}', encodeURIComponent(key)) : null;
};

export const isTrackerHost = (host: string): boolean => {
  const { trackerHosts, trackerIssueUrl } = appConfig();
  return trackerHosts.includes(host) || hostOf(trackerIssueUrl) === host;
};

export const taskKeyRe = (): RegExp => new RegExp(`^(?:${appConfig().taskKeyPattern})$`);

export const gitlabHosts = (): string[] => appConfig().gitlabHosts;

/** The documents service (ly-solutions/artifacts) and the boards service (ly-solutions/boards). */
export const artifactsHost = (): string | null => hostOf(appConfig().artifactsUrl);
export const boardsHost = (): string | null => hostOf(appConfig().boardsUrl);
const originOf = (url: string | null): string | null => {
  if (!url) return null;
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
};
export const artifactsOrigin = (): string | null => originOf(appConfig().artifactsUrl);
export const boardsOrigin = (): string | null => originOf(appConfig().boardsUrl);

/**
 * Whether an address is on the documents service / the boards service, by origin: the scheme and the port count,
 * so a workspace on http://localhost with the two services on other ports (the compose file) works as well as one
 * with a host each.
 */
export const isArtifactsUrl = (url: URL): boolean => url.origin === artifactsOrigin();
export const isBoardsUrl = (url: URL): boolean => url.origin === boardsOrigin();

/** This page's host, as people know it ("open it on …"). */
export const appHost = (): string =>
  typeof window === 'undefined' ? appConfig().productName : window.location.host;

/** An issue or pull request of the configured GitHub: `owner/repo#12`. */
export const GITHUB_KEY_RE =
  /^([A-Za-z0-9][A-Za-z0-9._-]{0,99})\/([A-Za-z0-9][A-Za-z0-9._-]{0,99})#([1-9]\d{0,8})$/;
const GITHUB_PATH_RE =
  /^\/([A-Za-z0-9][A-Za-z0-9._-]{0,99})\/([A-Za-z0-9][A-Za-z0-9._-]{0,99})\/(issues|pull)\/([1-9]\d{0,8})\/?$/;

export interface IGithubRef {
  owner: string;
  repo: string;
  number: number;
  pull: boolean;
}

/** The issue or pull request a GitHub link names, or null. */
export const githubRefOf = (href: string): IGithubRef | null => {
  let url: URL;
  try {
    url = new URL(href);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.hostname !== appConfig().githubHost || url.username || url.port)
    return null;
  const m = GITHUB_PATH_RE.exec(url.pathname);
  return m ? { owner: m[1]!, repo: m[2]!, number: Number(m[4]), pull: m[3] === 'pull' } : null;
};

export const githubKeyOf = (ref: IGithubRef): string => `${ref.owner}/${ref.repo}#${ref.number}`;

/** The page of a GitHub issue key. */
export const githubIssueUrl = (key: string): string | null => {
  const m = GITHUB_KEY_RE.exec(key);
  return m ? `https://${appConfig().githubHost}/${m[1]}/${m[2]}/issues/${m[3]}` : null;
};
