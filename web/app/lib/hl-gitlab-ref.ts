/** Pure GitLab references and shapes, shared by the runtime (`hl-gitlab`) and the pages. */

import { gitlabUrlOf, type TGitlabHost } from '@/lib/gitlab-hosts';

export type TPipelineStatus =
  'running' | 'pending' | 'success' | 'failed' | 'canceled' | 'skipped' | 'manual' | 'created' | 'other';

export interface IPipeline {
  status: TPipelineStatus;
  url: string;
}

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

/** A merge request or a commit of GitLab, read through the API to show it as a tab: GitLab itself refuses frames. */
export type TGitLabRef =
  | { kind: 'mr'; host: TGitlabHost; project: string; iid: number }
  | { kind: 'commit'; host: TGitlabHost; project: string; sha: string };

export interface IGitLabView {
  kind: TGitLabRef['kind'];
  url: string;
  host: TGitlabHost;
  project: string;
  /** `!42` or the short sha. */
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
}

export const gitlabRefOf = (raw: string): TGitLabRef | null => {
  const mr = mrRefOf(raw);
  if (mr) return { kind: 'mr', ...mr };
  const url = gitlabUrlOf(raw);
  const commit = url?.pathname.match(COMMIT_PATH);
  return url && commit ? { kind: 'commit', host: url.hostname, project: commit[1], sha: commit[2] } : null;
};
