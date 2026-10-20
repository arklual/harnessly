const TEXT_NODE = 3;
const ELEMENT_NODE = 1;
const BLOCK_TAGS = new Set(['DIV', 'P', 'LI', 'BLOCKQUOTE', 'PRE', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6']);

/** The part of a DOM node the reader needs; a plain object tree stands in for it in tests. */
export interface IEditableNode {
  nodeType: number;
  nodeName: string;
  nodeValue: string | null;
  childNodes: ArrayLike<IEditableNode>;
}

/**
 * The text of a rich contentEditable the way the person sees it. Chrome turns Enter into `<div>` lines and keeps a
 * `<br>` in an empty line as a placeholder: `textContent` loses the breaks and `innerText` counts the placeholders.
 */
export const readEditableText = (root: IEditableNode): string => {
  let out = '';
  let lineOpen = false;
  const walk = (node: IEditableNode) => {
    const children = Array.from(node.childNodes);
    children.forEach((child, index) => {
      if (child.nodeType === TEXT_NODE) {
        if (lineOpen) out += '\n';
        lineOpen = false;
        out += child.nodeValue ?? '';
        return;
      }
      if (child.nodeType !== ELEMENT_NODE) return;
      if (child.nodeName === 'BR') {
        if (index !== children.length - 1) out += '\n';
        return;
      }
      if (BLOCK_TAGS.has(child.nodeName)) {
        if (out !== '' || lineOpen) out += '\n';
        lineOpen = false;
        walk(child);
        lineOpen = true;
        return;
      }
      walk(child);
    });
  };
  walk(root);
  return out;
};
