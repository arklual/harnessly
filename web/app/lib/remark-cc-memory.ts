import type { Html, Parent, Root, RootContent } from 'mdast';

const OPEN_RE = /^<cc-memory\b([^>]*)>$/;
const CLOSE_RE = /^<\/cc-memory\s*>$/;
const ANY_TAG_RE = /<\/?cc-memory\b[^>]*>/g;
const WHOLE_RE = /^<cc-memory\b([^>]*)>([\s\S]*?)<\/cc-memory\s*>$/;
const BLOCK_PARENTS = new Set(['root', 'listItem', 'blockquote', 'footnoteDefinition']);

export const memoryFilesOf = (attrs: string): string[] =>
  (attrs.match(/\bfilenames="([^"]*)"/)?.[1] ?? '')
    .split(',')
    .map((name) => name.trim())
    .filter(Boolean);

const citation = (files: string[], children: RootContent[]) =>
  ({
    type: 'ccMemory',
    children,
    data: { hName: 'cc-memory', hProperties: { files: files.join(',') } },
  }) as unknown as RootContent;

const isHtml = (node: RootContent): node is Html => node.type === 'html';

const transform = (parent: Parent) => {
  const out: RootContent[] = [];
  const children = parent.children as RootContent[];
  for (let i = 0; i < children.length; i++) {
    const node = children[i];
    if (!isHtml(node)) {
      if ('children' in node) transform(node as Parent);
      out.push(node);
      continue;
    }
    const open = node.value.trim().match(OPEN_RE);
    if (open) {
      const close = children.findIndex(
        (next, j) => j > i && isHtml(next) && CLOSE_RE.test(next.value.trim()),
      );
      if (close === -1) {
        // The closing tag landed in another block: keep the marker where the citation starts.
        out.push(citation(memoryFilesOf(open[1]), []));
        continue;
      }
      const inner = { type: 'paragraph', children: children.slice(i + 1, close) } as Parent;
      transform(inner);
      out.push(citation(memoryFilesOf(open[1]), inner.children as RootContent[]));
      i = close;
      continue;
    }
    if (CLOSE_RE.test(node.value.trim())) continue;
    if (!node.value.includes('cc-memory')) {
      out.push(node);
      continue;
    }
    // A tag alone on its line opens a raw HTML block, so the whole citation arrives as one html node.
    const whole = node.value.trim().match(WHOLE_RE);
    const inline = whole
      ? citation(memoryFilesOf(whole[1]), [{ type: 'text', value: whole[2].trim() } as RootContent])
      : ({ type: 'text', value: node.value.replace(ANY_TAG_RE, '').trim() } as RootContent);
    out.push(
      BLOCK_PARENTS.has(parent.type) ? ({ type: 'paragraph', children: [inline] } as RootContent) : inline,
    );
  }
  parent.children = out as Parent['children'];
};

/** Turns Claude Code's `<cc-memory filenames="…">sentence</cc-memory>` citations into `cc-memory` elements instead of escaped text. */
const remarkCcMemory = () => (tree: Root) => transform(tree);

export default remarkCcMemory;
