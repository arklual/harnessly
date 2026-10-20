// Shapes of what the runtime answers; the runtime itself is Go.

export interface ICommitSummary {
  sha: string;
  short: string;
  subject: string;
  author: string;
  date: number;
  additions: number;
  deletions: number;
  files: number;
}

export interface IRepoChanges {
  root: string;
  name: string;
  branch: string | null;
  diff: string;
  truncated: boolean;
  commits: ICommitSummary[];
  commitsTruncated: boolean;
}
