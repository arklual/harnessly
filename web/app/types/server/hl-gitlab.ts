// Shapes of what the runtime answers; the runtime itself is Go.
import type { TGitlabHost } from '@/lib/gitlab-hosts';
import type { IPipeline } from '@/lib/hl-gitlab-ref';

export interface IGitLabMr {
  host: TGitlabHost;
  project: string;
  iid: number;
  title: string;
  state: string;
  draft: boolean;
  url: string;
  pipeline: IPipeline | null;
}

export interface IGitLabPush {
  host: TGitlabHost;
  project: string;
  branch: string;
  commitTitle: string | null;
  pushedAt: string | null;
  /** Local commits on this branch that GitLab has not seen yet. */
  unpushed: number;
  pipeline: IPipeline | null;
}
