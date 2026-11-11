// The work of a chat in git: the worktrees, branches, commits and merge requests the chat and the chats it delegated
// worked on, as the machine's registry (POST <platform>/work) and the hub's copy (GET <gateway>/history/conversations/
// <id>/work) have them; and the change sets a diff viewer shows of each: from the machine while it is on, from the code
// host when the branch is pushed and the machine is not.

import { appFetch, scopedAppPath } from '@/lib/app-path';
import { parseMultiFileDiff } from '@/lib/parse-git-diff';
import { gatewayPath } from '@/next/core/net/machine';
import type { ICommitSummary, IRepoChanges } from '@/types/server/hl-changes';

export interface IWorkEntry {
  conv: string;
  parent?: string;
  repo: string;
  worktree: string;
  branch?: string;
  base?: string;
  head?: string;
  commits?: string[];
  remote?: string;
  mr?: string;
  dirty?: boolean;
  since: number;
  at: number;
  source: 'live' | 'scan';
  /** The machine the worktree is on; absent for this page's own machine. */
  machineId?: string;
}

export interface IWorkMachine {
  id: string;
  name: string;
  state: 'online' | 'offline';
}

export interface IChatWork {
  entries: IWorkEntry[];
  machines: IWorkMachine[];
  /** False while old chats' transcripts are still being looked through. */
  complete: boolean;
}

const keyOf = (e: IWorkEntry) => `${e.conv}\u0000${e.worktree}`;

/** The two answers as one list: one record per chat and worktree, the newer look kept, the newest first. */
export const mergeWork = (local: IWorkEntry[], hub: IWorkEntry[]): IWorkEntry[] => {
  const byKey = new Map<string, IWorkEntry>();
  for (const e of [...hub, ...local]) {
    const prev = byKey.get(keyOf(e));
    if (!prev || e.at > prev.at) byKey.set(keyOf(e), { ...e, machineId: e.machineId ?? prev?.machineId });
  }
  return [...byKey.values()].sort((a, b) => b.at - a.at);
};

const readJSON = async <T>(res: Promise<Response>): Promise<T | null> => {
  try {
    const r = await res;
    return r.ok ? ((await r.json()) as T) : null;
  } catch {
    return null;
  }
};

/** The work of a chat and its delegated chats. Either source may be missing (no hub; the machine is off). */
export const fetchChatWork = async (conversationId: string): Promise<IChatWork | null> => {
  const [local, hub] = await Promise.all([
    readJSON<{ entries: IWorkEntry[]; complete: boolean }>(
      appFetch('/api/platform/work', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ conversationId }),
      }),
    ),
    readJSON<{ entries: IWorkEntry[]; machines: IWorkMachine[] }>(
      fetch(gatewayPath(`/history/conversations/${encodeURIComponent(conversationId)}/work`), {
        credentials: 'same-origin',
      }),
    ),
  ]);
  if (!local && !hub) return null;
  return {
    entries: mergeWork(local?.entries ?? [], hub?.entries ?? []),
    machines: hub?.machines ?? [],
    complete: local?.complete ?? true,
  };
};

export interface IWorkRepo {
  /** `<machine>\u0000<repo>`. */
  key: string;
  repo: string;
  name: string;
  machineId?: string;
  worktrees: IWorkEntry[];
}

/** Repositories, then their worktrees (a branch each), newest work first. */
export const groupWork = (entries: IWorkEntry[]): IWorkRepo[] => {
  const repos = new Map<string, IWorkRepo>();
  for (const e of entries) {
    const key = `${e.machineId ?? ''}\u0000${e.repo}`;
    let r = repos.get(key);
    if (!r) {
      r = {
        key,
        repo: e.repo,
        name: e.repo.replace(/\/+$/, '').split('/').at(-1) ?? e.repo,
        machineId: e.machineId,
        worktrees: [],
      };
      repos.set(key, r);
    }
    const same = r.worktrees.find((w) => w.worktree === e.worktree);
    if (!same) r.worktrees.push(e);
    else if (e.since < same.since) Object.assign(same, { since: e.since, base: e.base ?? same.base });
  }
  return [...repos.values()];
};

/** The short name of a merge request: `!12`. */
export const mrLabel = (url: string): string => {
  const m = /\/-\/merge_requests\/(\d+)/.exec(url);
  return m ? `!${m[1]}` : url;
};

// --- change sets: what a diff viewer is given ---

export type TChangedFileStatus = 'added' | 'deleted' | 'renamed' | 'modified';

/** One file of a change set: its paths, counts and patch (a unified diff from its `diff --git` line). */
export interface IChangedFile {
  path: string;
  oldPath: string;
  status: TChangedFileStatus;
  additions: number;
  deletions: number;
  binary: boolean;
  patch: string;
}

/** A diff with what it is a diff of: a viewer gets the files and the metadata, the old list gets `diff`. */
export interface IChangeSet {
  source: 'machine' | 'gitlab';
  repo: string;
  branch: string | null;
  base: string | null;
  head: string | null;
  mr: string | null;
  commits: ICommitSummary[];
  /** The whole diff text (uncommitted work on a machine; the branch against its base on a code host). */
  diff: string;
  files: IChangedFile[];
  truncated: boolean;
}

export const changedFiles = (diff: string): IChangedFile[] =>
  (diff ? parseMultiFileDiff(diff) : []).map((f) => {
    let status: TChangedFileStatus = 'modified';
    if (f.isNew) status = 'added';
    else if (f.isDeleted) status = 'deleted';
    else if (f.oldPath !== f.newPath) status = 'renamed';
    return {
      path: f.isDeleted ? f.oldPath : f.newPath,
      oldPath: f.oldPath,
      status,
      additions: f.additions,
      deletions: f.deletions,
      binary: f.isBinary,
      patch: f.raw,
    };
  });

/** The worktree's commits and uncommitted work, read on its machine. */
export const fetchMachineChanges = async (entry: IWorkEntry): Promise<IChangeSet | null> => {
  const res = await readJSON<{ repos: IRepoChanges[] }>(
    fetch(scopedAppPath('/api/platform/changes', entry.machineId), {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths: [entry.worktree], sinceMs: entry.since }),
    }),
  );
  const repo = res?.repos.find((r) => r.root === entry.worktree) ?? res?.repos[0];
  if (!repo) return null;
  return {
    source: 'machine',
    repo: entry.repo,
    branch: repo.branch,
    base: entry.base ?? null,
    head: entry.head ?? null,
    mr: entry.mr ?? null,
    commits: repo.commits,
    diff: repo.diff,
    files: changedFiles(repo.diff),
    truncated: repo.truncated || repo.commitsTruncated,
  };
};

interface IRemoteBranch {
  branch: {
    commits: { sha: string; short: string; subject: string; author: string; date?: string }[];
    truncated: boolean;
    mr: { url: string } | null;
  };
  diff: string;
}

export class RemoteBranchError extends Error {}

/** A pushed branch against its base, read from its code host through this machine's connection. */
export const fetchRemoteChanges = async (entry: IWorkEntry): Promise<IChangeSet> => {
  if (!entry.remote || !entry.branch) throw new RemoteBranchError('not-pushed');
  const r = await appFetch('/api/platform/work/remote', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ remote: entry.remote, branch: entry.branch, base: '' }),
  });
  if (!r.ok) {
    const body = (await r.json().catch(() => null)) as { error?: string } | null;
    throw new RemoteBranchError(body?.error ?? String(r.status));
  }
  const got = (await r.json()) as IRemoteBranch;
  return {
    source: 'gitlab',
    repo: entry.repo,
    branch: entry.branch,
    base: entry.base ?? null,
    head: entry.head ?? null,
    mr: got.branch.mr?.url ?? entry.mr ?? null,
    commits: got.branch.commits.map((c) => ({
      sha: c.sha,
      short: c.short,
      subject: c.subject,
      author: c.author,
      date: c.date ? Date.parse(c.date) : 0,
      additions: 0,
      deletions: 0,
      files: 0,
    })),
    diff: got.diff,
    files: changedFiles(got.diff),
    truncated: got.branch.truncated,
  };
};
