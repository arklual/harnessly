// The page's DOM as the site's DevTools hold it: the tree of tags the board's bridge told, in pieces, and the rows the
// «Elements» tab draws of the opened part of it. A node is addressed by its path — the index of each element child from
// the document's root down — which is all the bridge needs to find it again. No React here.

/** The element children's indices from the root element; `[]` is the root. */
export type TDomPath = readonly number[];

export interface IDomNode {
  tag: string;
  id: string;
  /** The class attribute, as the page wrote it. */
  cls: string;
  /** A few words of the node's own text. */
  text: string;
  /** How many element children it has, told or not. */
  n: number;
  /** The children told so far; null while the node has not been asked for (it may still have `n`). */
  kids: IDomNode[] | null;
  /** Children left out of `kids` because the bridge keeps a branch short. */
  more: number;
}

export const pathKey = (path: TDomPath) => path.join('.');

export const nodeAt = (root: IDomNode | null, path: TDomPath): IDomNode | null => {
  let node = root;
  for (const index of path) {
    node = node?.kids?.[index] ?? null;
    if (node === null) return null;
  }
  return node;
};

/** The tree with the branch at `path` replaced by what the bridge told; a path with a part not told yet changes nothing. */
export const withNode = (root: IDomNode | null, path: TDomPath, node: IDomNode): IDomNode | null => {
  if (path.length === 0) return node;
  if (root === null || root.kids === null) return root;
  const [index, ...rest] = path;
  const child = root.kids[index];
  if (!child) return root;
  const next = withNode(child, rest, node);
  if (next === child) return root;
  const kids = [...root.kids];
  kids[index] = next ?? child;
  return { ...root, kids };
};

/** `div#hero.card.wide`: how a node is called, as a selector reads. */
export const selectorOf = (node: Pick<IDomNode, 'tag' | 'id' | 'cls'>) => {
  const classes = node.cls.split(/\s+/).filter(Boolean);
  return `${node.tag}${node.id ? `#${node.id}` : ''}${classes.map((c) => `.${c}`).join('')}`;
};

/** What follows the tag in a row: its id and classes. */
export const markOf = (node: Pick<IDomNode, 'id' | 'cls'>) =>
  selectorOf({ id: node.id, cls: node.cls, tag: '' });

export interface IDomRow {
  key: string;
  path: TDomPath;
  depth: number;
  node: IDomNode;
  /** The node has children to show or to ask for. */
  expandable: boolean;
  open: boolean;
}

/** The rows of the opened part of the tree, in the order a person reads it. */
export const domRows = (root: IDomNode | null, open: ReadonlySet<string>): IDomRow[] => {
  const rows: IDomRow[] = [];
  const walk = (node: IDomNode, path: number[]) => {
    const key = pathKey(path);
    const isOpen = node.n > 0 && open.has(key);
    rows.push({ key, path, depth: path.length, node, expandable: node.n > 0, open: isOpen });
    if (!isOpen || node.kids === null) return;
    node.kids.forEach((kid, index) => walk(kid, [...path, index]));
  };
  if (root) walk(root, []);
  return rows;
};

/** Every path from the root down to `path`, the root first and `path` last. */
export const pathsTo = (path: TDomPath): number[][] =>
  path.map((_, i) => path.slice(0, i)).concat([[...path]]);
