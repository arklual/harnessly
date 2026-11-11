import { describe, expect, it } from 'vitest';
import { changedFiles, groupWork, mergeWork, mrLabel, type IWorkEntry } from '@/lib/chat-work';

const entry = (over: Partial<IWorkEntry>): IWorkEntry => ({
  conv: 'tab-a',
  repo: '/r',
  worktree: '/r',
  since: 1,
  at: 1,
  source: 'live',
  ...over,
});

describe('mergeWork', () => {
  it('keeps one record per chat and worktree, the newer look, with the machine the hub named', () => {
    const local = [entry({ conv: 'tab-child', worktree: '/r/.worktrees/x', at: 5, branch: 'agent/x' })];
    const hub = [
      entry({ conv: 'tab-child', worktree: '/r/.worktrees/x', at: 3, branch: 'old', machineId: 'm-1' }),
      entry({ conv: 'tab-grand', worktree: '/s', repo: '/s', at: 9, machineId: 'm-2' }),
    ];
    const got = mergeWork(local, hub);
    expect(got.map((e) => e.conv)).toEqual(['tab-grand', 'tab-child']);
    expect(got[1]).toMatchObject({ branch: 'agent/x', machineId: 'm-1' });
  });
});

describe('groupWork', () => {
  it('groups worktrees by machine and repository', () => {
    const groups = groupWork([
      entry({ worktree: '/r/.worktrees/a', branch: 'a' }),
      entry({ worktree: '/r/.worktrees/b', branch: 'b' }),
      entry({ repo: '/r', worktree: '/r', machineId: 'm-2' }),
    ]);
    expect(groups.map((g) => [g.name, g.machineId, g.worktrees.length])).toEqual([
      ['r', undefined, 2],
      ['r', 'm-2', 1],
    ]);
  });
});

describe('mrLabel', () => {
  it('names a merge request by its iid', () => {
    expect(mrLabel('https://gitlab.example.com/g/p/-/merge_requests/909')).toBe('!909');
  });
});

describe('changedFiles', () => {
  it('reads a diff into files a viewer takes', () => {
    const diff =
      'diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n' +
      'diff --git a/n.go b/n.go\nnew file mode 100644\n--- /dev/null\n+++ b/n.go\n@@ -0,0 +1 @@\n+z\n';
    const files = changedFiles(diff);
    expect(files.map((f) => [f.path, f.status, f.additions, f.deletions])).toEqual([
      ['a.go', 'modified', 1, 1],
      ['n.go', 'added', 1, 0],
    ]);
    expect(files[1].patch.startsWith('diff --git a/n.go')).toBe(true);
  });
});
