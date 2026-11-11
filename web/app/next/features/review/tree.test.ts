import { describe, expect, it } from 'vitest';
import { FILE_STATUS, type IReviewFile } from '@/next/features/review/model';
import { ancestorsOf, buildTree, dirPaths, matchesFilter, treeOrder } from '@/next/features/review/tree';

const file = (path: string, additions = 1, deletions = 0): IReviewFile => ({
  path,
  oldPath: path,
  status: FILE_STATUS.modified,
  additions,
  deletions,
  binary: false,
  patch: '',
  version: 'v1',
});

const files = [
  file('web/app/next/b.tsx', 3, 1),
  file('web/app/next/a.tsx', 2, 0),
  file('README.md', 1, 1),
  file('internal/hub/x.go', 5, 5),
  file('internal/files/y.go', 1, 0),
];

describe('the tree of changed files', () => {
  it('puts folders before files, sorted by name, and merges single-folder chains', () => {
    const root = buildTree(files);
    expect(root.children.map((n) => n.name)).toEqual(['internal', 'web/app/next', 'README.md']);
    const internal = root.children[0]!;
    expect(internal.kind === 'dir' && internal.children.map((n) => n.name)).toEqual(['files', 'hub']);
  });

  it('sums the added and removed lines and the files of each folder', () => {
    const root = buildTree(files);
    const internal = root.children[0]!;
    expect(internal.kind === 'dir' && [internal.additions, internal.deletions, internal.files]).toEqual([
      6, 5, 2,
    ]);
    expect([root.additions, root.deletions, root.files]).toEqual([12, 7, 5]);
  });

  it('walks the files in the order the tree shows them', () => {
    expect(treeOrder(buildTree(files)).map((f) => f.path)).toEqual([
      'internal/files/y.go',
      'internal/hub/x.go',
      'web/app/next/a.tsx',
      'web/app/next/b.tsx',
      'README.md',
    ]);
  });

  it('filters by a part of the path, ignoring case, and drops emptied folders', () => {
    const root = buildTree(files, 'HUB');
    expect(treeOrder(root).map((f) => f.path)).toEqual(['internal/hub/x.go']);
    expect(root.children.map((n) => n.name)).toEqual(['internal/hub']);
    expect(matchesFilter(file('a/b.ts'), '  ')).toBe(true);
    expect(matchesFilter({ ...file('new/name.ts'), oldPath: 'old/thing.ts' }, 'thing')).toBe(true);
  });

  it('knows the folders to open for a file and every folder there is', () => {
    const root = buildTree(files);
    expect(ancestorsOf(root, 'internal/hub/x.go')).toEqual(['internal', 'internal/hub']);
    expect(ancestorsOf(root, 'web/app/next/a.tsx')).toEqual(['web/app/next']);
    expect(ancestorsOf(root, 'README.md')).toEqual([]);
    expect(dirPaths(root)).toEqual(['internal', 'internal/files', 'internal/hub', 'web/app/next']);
  });

  it('orders numbered names as numbers', () => {
    const root = buildTree([file('m/10.sql'), file('m/2.sql'), file('m/1.sql')]);
    expect(treeOrder(root).map((f) => f.path)).toEqual(['m/1.sql', 'm/2.sql', 'm/10.sql']);
  });
});
