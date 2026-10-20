import type { Parent, Root, RootContent, Text } from 'mdast';

import { issueKeyRe, issueUrl } from '@/lib/tracker-keys';

const SKIP = new Set(['link', 'linkReference', 'inlineCode', 'code', 'definition', 'html']);

const splitText = (node: Text): RootContent[] => {
  const out: RootContent[] = [];
  let last = 0;
  for (const match of node.value.matchAll(issueKeyRe())) {
    if (match.index > last) out.push({ type: 'text', value: node.value.slice(last, match.index) });
    out.push({
      type: 'link',
      url: issueUrl(match[1]),
      children: [{ type: 'text', value: match[1] }],
      data: { hProperties: { 'data-issue-key': match[1] } },
    } as RootContent);
    last = match.index + match[0].length;
  }
  if (last === 0) return [node];
  if (last < node.value.length) out.push({ type: 'text', value: node.value.slice(last) });
  return out;
};

const walk = (parent: Parent) => {
  parent.children = (parent.children as RootContent[]).flatMap((node) => {
    if (SKIP.has(node.type)) return [node];
    if (node.type === 'text') return splitText(node);
    if ('children' in node) walk(node as Parent);
    return [node];
  }) as Parent['children'];
};

/** A bare task key in an answer becomes a tracker link, so it renders as the live task chip. */
const remarkIssueKeys = () => (tree: Root) => walk(tree);

export default remarkIssueKeys;
