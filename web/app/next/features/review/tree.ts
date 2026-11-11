// The changed files as a folder tree: folders first, then files, by name; a folder with a single folder inside reads as
// one row (`web/app/next`), as IDEs show it. Each folder carries the sum of its files' added and removed lines.
import type { IReviewFile } from '@/next/features/review/model';

export interface IDirNode {
  kind: 'dir';
  /** The row's label: one folder name or a chain of them (`a/b/c`). */
  name: string;
  /** The folder's full path, the key of its open/closed state. */
  path: string;
  children: TTreeNode[];
  additions: number;
  deletions: number;
  files: number;
}

export interface IFileNode {
  kind: 'file';
  name: string;
  file: IReviewFile;
}

export type TTreeNode = IDirNode | IFileNode;

interface IBuildDir {
  dirs: Map<string, IBuildDir>;
  files: IReviewFile[];
}

const emptyDir = (): IBuildDir => ({ dirs: new Map(), files: [] });

const byName = (a: string, b: string) => a.localeCompare(b, 'en', { numeric: true, sensitivity: 'base' });

const close = (name: string, path: string, dir: IBuildDir): IDirNode => {
  const dirs = [...dir.dirs.entries()]
    .sort(([a], [b]) => byName(a, b))
    .map(([child, sub]) => close(child, path ? `${path}/${child}` : child, sub));
  const files: IFileNode[] = [...dir.files]
    .sort((a, b) => byName(baseName(a.path), baseName(b.path)))
    .map((file) => ({ kind: 'file', name: baseName(file.path), file }));
  const node: IDirNode = {
    kind: 'dir',
    name,
    path,
    children: [...dirs, ...files],
    additions: dirs.reduce((s, d) => s + d.additions, 0) + files.reduce((s, f) => s + f.file.additions, 0),
    deletions: dirs.reduce((s, d) => s + d.deletions, 0) + files.reduce((s, f) => s + f.file.deletions, 0),
    files: dirs.reduce((s, d) => s + d.files, 0) + files.length,
  };
  // A folder whose only child is a folder merges with it into one row.
  const only = node.children.length === 1 ? node.children[0] : undefined;
  if (path && only?.kind === 'dir') return { ...only, name: `${name}/${only.name}` };
  return node;
};

export const baseName = (path: string): string => path.slice(path.lastIndexOf('/') + 1);

/** Whether a file's path holds the filter text, ignoring case; an empty filter keeps every file. */
export const matchesFilter = (file: IReviewFile, filter: string): boolean => {
  const needle = filter.trim().toLowerCase();
  if (!needle) return true;
  return file.path.toLowerCase().includes(needle) || file.oldPath.toLowerCase().includes(needle);
};

/** The tree of the files that pass the filter; the root itself is not a row. */
export const buildTree = (files: readonly IReviewFile[], filter = ''): IDirNode => {
  const root = emptyDir();
  for (const file of files) {
    if (!matchesFilter(file, filter)) continue;
    const parts = file.path.split('/');
    parts.pop();
    let dir = root;
    for (const part of parts) {
      let next = dir.dirs.get(part);
      if (!next) {
        next = emptyDir();
        dir.dirs.set(part, next);
      }
      dir = next;
    }
    dir.files.push(file);
  }
  return close('', '', root);
};

/** The files in the order the tree shows them: the order F7 walks and the list a narrow screen shows. */
export const treeOrder = (root: IDirNode): IReviewFile[] => {
  const out: IReviewFile[] = [];
  const walk = (node: TTreeNode) => {
    if (node.kind === 'file') out.push(node.file);
    else node.children.forEach(walk);
  };
  walk(root);
  return out;
};

/** Every folder path of the tree: what «expand all» opens. */
export const dirPaths = (root: IDirNode): string[] => {
  const out: string[] = [];
  const walk = (node: TTreeNode) => {
    if (node.kind !== 'dir') return;
    if (node.path) out.push(node.path);
    node.children.forEach(walk);
  };
  walk(root);
  return out;
};

/** The folders above a file, by the paths the tree uses for them: what must be open for the file to show. */
export const ancestorsOf = (root: IDirNode, filePath: string): string[] => {
  const out: string[] = [];
  const walk = (node: IDirNode): boolean => {
    for (const child of node.children) {
      if (child.kind === 'file') {
        if (child.file.path === filePath) return true;
      } else if (walk(child)) {
        out.push(child.path);
        return true;
      }
    }
    return false;
  };
  walk(root);
  return out.reverse();
};
