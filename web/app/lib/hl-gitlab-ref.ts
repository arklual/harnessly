/** Pure GitLab references and shapes, shared by the runtime (`hl-gitlab`) and the pages. */

import { gitlabUrlOf, type TGitlabHost } from '@/lib/gitlab-hosts';
import { appConfig } from '@/platform/config';

export type TPipelineStatus =
  'running' | 'pending' | 'success' | 'failed' | 'canceled' | 'skipped' | 'manual' | 'created' | 'other';

export interface IPipeline {
  status: TPipelineStatus;
  url: string;
}

const githubHost = (): string => appConfig().githubHost;

export interface IMrRef {
  host: TGitlabHost;
  project: string;
  iid: number;
}

const MR_PATH = /^\/(.+?)\/-\/merge_requests\/(\d+)/;
const COMMIT_PATH = /^\/(.+?)\/-\/commit\/([0-9a-f]{7,40})(?:\/|$)/;

export const mrRefOf = (raw: string): IMrRef | null => {
  const url = gitlabUrlOf(raw);
  const match = url?.pathname.match(MR_PATH);
  return url && match ? { host: url.hostname, project: match[1], iid: Number(match[2]) } : null;
};

export type TChangeProvider = 'gitlab' | 'github';

/**
 * A merge request, a pull request, a commit or an issue, read through the service's API to show it as a tab: GitLab
 * and GitHub both refuse frames. The tab is the same for both; `provider` says which one the link is on.
 */
export type TGitLabRef = { provider: TChangeProvider; host: string; project: string } & (
  { kind: 'mr' | 'issue'; iid: number } | { kind: 'commit'; sha: string }
);

/** Why there is no more than the link to show: no token on the machine, the token is refused, the object is not
 *  visible to it (a private repository), the hourly allowance is spent, GitHub does not answer. */
export type TChangeLimit = 'no-token' | 'bad-token' | 'not-found' | 'rate-limit' | 'unavailable';

export interface IChangeJob {
  name: string;
  status: TPipelineStatus;
  /** The job's own page (its log): the service refuses frames, so it opens in a new browser tab. */
  url: string | null;
  durationMs: number | null;
}

/** A stage of a GitLab pipeline, or a workflow of GitHub checks. */
export interface IChangeStage {
  name: string;
  status: TPipelineStatus;
  url: string | null;
  jobs: IChangeJob[];
}

export interface IChangeNote {
  author: string | null;
  at: string | null;
  body: string;
}

/** A comment, or a discussion with its replies. `resolved` is null for what cannot be resolved. */
export interface IChangeThread {
  id: string;
  resolved: boolean | null;
  /** The file and line a comment on the diff is about. */
  path: string | null;
  line: number | null;
  /** The line is no longer in the diff. */
  outdated: boolean;
  notes: IChangeNote[];
}

export interface IChangeView {
  kind: TGitLabRef['kind'];
  provider: TChangeProvider;
  url: string;
  host: string;
  project: string;
  /** `!42`, `#42` or the short sha. */
  ref: string;
  title: string;
  /** The description of a merge request, the body of a commit message. */
  description: string;
  author: string | null;
  at: string | null;
  state: string | null;
  draft: boolean;
  sourceBranch: string | null;
  targetBranch: string | null;
  pipeline: IPipeline | null;
  diff: string;
  /** The diff is cut: more files, or more bytes, than a tab shows. */
  truncated: boolean;
  labels: string[];
  /** The stages of the pipeline with their jobs; null when there is none or it could not be read. */
  stages: IChangeStage[] | null;
  /** The discussion; null when it could not be read. */
  comments: IChangeThread[] | null;
  /** Set when the view is only a card made of the link, and says why. */
  limited: TChangeLimit | null;
}

const GITHUB_NAME = '[A-Za-z0-9._-]{1,100}';
const GITHUB_PULL = new RegExp(`^/(${GITHUB_NAME}/${GITHUB_NAME})/pull/([1-9]\\d{0,8})(?:/.*)?$`);
const GITHUB_ISSUE = new RegExp(`^/(${GITHUB_NAME}/${GITHUB_NAME})/issues/([1-9]\\d{0,8})/?$`);
const GITHUB_COMMIT = new RegExp(`^/(${GITHUB_NAME}/${GITHUB_NAME})/commits?/([0-9a-fA-F]{7,40})/?$`);

/** A pull request, a commit or an issue of the deployment's GitHub (github.com by default). */
export const githubChangeOf = (raw: string): TGitLabRef | null => {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.username || url.password || url.port || url.hostname !== githubHost())
    return null;
  const base = { provider: 'github', host: url.hostname } as const;
  const pull = GITHUB_PULL.exec(url.pathname);
  if (pull) return { ...base, kind: 'mr', project: pull[1]!, iid: Number(pull[2]) };
  const issue = GITHUB_ISSUE.exec(url.pathname);
  if (issue) return { ...base, kind: 'issue', project: issue[1]!, iid: Number(issue[2]) };
  const commit = GITHUB_COMMIT.exec(url.pathname);
  return commit ? { ...base, kind: 'commit', project: commit[1]!, sha: commit[2]!.toLowerCase() } : null;
};

export const gitlabRefOf = (raw: string): TGitLabRef | null => {
  const github = githubChangeOf(raw);
  if (github) return github;
  const mr = mrRefOf(raw);
  if (mr) return { provider: 'gitlab', kind: 'mr', ...mr };
  const url = gitlabUrlOf(raw);
  const commit = url?.pathname.match(COMMIT_PATH);
  return url && commit
    ? { provider: 'gitlab', kind: 'commit', host: url.hostname, project: commit[1], sha: commit[2] }
    : null;
};

/** What the tab of a change is called: `app !12`, `app #12`, `app 1a2b3c4d`. */
export const changeTitleOf = (ref: TGitLabRef, shaLength: number): string => {
  const repo = ref.project.split('/').at(-1);
  if (ref.kind === 'commit') return `${repo} ${ref.sha.slice(0, shaLength)}`;
  return `${repo} ${ref.provider === 'github' ? '#' : '!'}${ref.iid}`;
};
