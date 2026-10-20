const TEXT_NODE = 3;
const ELEMENT_NODE = 1;

/** The part of a DOM node the converter reads; a plain object tree stands in for it in tests. */
export interface IHtmlNode {
  nodeType: number;
  nodeName: string;
  nodeValue: string | null;
  childNodes: ArrayLike<IHtmlNode>;
  getAttribute?: (name: string) => string | null;
}

const SKIPPED = new Set([
  'SCRIPT',
  'STYLE',
  'META',
  'LINK',
  'TITLE',
  'HEAD',
  'TEMPLATE',
  'NOSCRIPT',
  'IMG',
  'SVG',
  'BUTTON',
  'INPUT',
]);
const PARAGRAPHS = new Set([
  'P',
  'H1',
  'H2',
  'H3',
  'H4',
  'H5',
  'H6',
  'UL',
  'OL',
  'PRE',
  'BLOCKQUOTE',
  'TABLE',
  'HR',
]);
const LINES = new Set([
  'DIV',
  'SECTION',
  'ARTICLE',
  'HEADER',
  'FOOTER',
  'MAIN',
  'ASIDE',
  'NAV',
  'FIGURE',
  'FIGCAPTION',
  'TR',
  'DT',
  'DD',
  'LI',
]);
/** Tags that carry formatting worth keeping; a paste with none of them keeps the source's plain text exactly. */
const FORMATTING = new Set([
  'A',
  'B',
  'STRONG',
  'I',
  'EM',
  'CODE',
  'PRE',
  'LI',
  'H1',
  'H2',
  'H3',
  'H4',
  'H5',
  'H6',
  'BLOCKQUOTE',
  'TABLE',
  'S',
  'DEL',
]);
const SAFE_HREF_RE = /^(?:https?:|mailto:)/i;
/** Google Docs wraps a whole copy in <b style="font-weight:normal">: bold only by name. */
const NOT_BOLD_RE = /font-weight\s*:\s*(?:normal|[1-4]00)\b/i;

const attr = (node: IHtmlNode, name: string): string => node.getAttribute?.(name) ?? '';

const wrap = (mark: string, text: string): string => {
  const inner = text.trim();
  if (!inner) return text;
  const lead = text.startsWith(' ') ? ' ' : '';
  const tail = text.endsWith(' ') ? ' ' : '';
  return `${lead}${mark}${inner}${mark}${tail}`;
};

const hasFormatting = (node: IHtmlNode): boolean => {
  if (
    node.nodeType !== ELEMENT_NODE &&
    node.nodeName !== '#document' &&
    node.nodeName !== '#document-fragment'
  )
    return false;
  if (FORMATTING.has(node.nodeName)) {
    if (node.nodeName === 'A') return SAFE_HREF_RE.test(attr(node, 'href'));
    if (node.nodeName === 'B' && NOT_BOLD_RE.test(attr(node, 'style')))
      return Array.from(node.childNodes).some(hasFormatting);
    return true;
  }
  return Array.from(node.childNodes).some(hasFormatting);
};

interface IContext {
  pre: boolean;
  listDepth: number;
}

const inline = (node: IHtmlNode, context: IContext): string => {
  if (node.nodeType === TEXT_NODE) {
    const text = node.nodeValue ?? '';
    return context.pre ? text : text.replace(/[\s\u00a0]+/g, ' ');
  }
  if (
    node.nodeType !== ELEMENT_NODE &&
    node.nodeName !== '#document' &&
    node.nodeName !== '#document-fragment'
  )
    return '';
  const name = node.nodeName;
  if (SKIPPED.has(name)) return '';
  const children = () =>
    Array.from(node.childNodes)
      .map((child) => inline(child, context))
      .join('');

  switch (name) {
    case 'BR':
      return '\n';
    case 'B':
    case 'STRONG':
      return NOT_BOLD_RE.test(attr(node, 'style')) ? children() : wrap('**', children());
    case 'I':
    case 'EM':
      return wrap('*', children());
    case 'S':
    case 'DEL':
      return wrap('~~', children());
    case 'CODE':
      return context.pre ? children() : wrap('`', children());
    case 'A': {
      const href = attr(node, 'href').trim();
      const text = children();
      if (!SAFE_HREF_RE.test(href)) return text;
      const label = text.trim();
      if (!label || label === href) return href;
      return `[${label}](${href})`;
    }
    case 'PRE': {
      const code = Array.from(node.childNodes)
        .map((child) => inline(child, { ...context, pre: true }))
        .join('')
        .replace(/\n+$/, '');
      return `\n\n\`\`\`\n${code}\n\`\`\`\n\n`;
    }
    case 'H1':
    case 'H2':
    case 'H3':
    case 'H4':
    case 'H5':
    case 'H6':
      return `\n\n${'#'.repeat(Number(name[1]))} ${children().trim()}\n\n`;
    case 'HR':
      return '\n\n---\n\n';
    case 'BLOCKQUOTE': {
      const quoted = children()
        .trim()
        .split('\n')
        .map((line) => (line ? `> ${line}` : '>'))
        .join('\n');
      return `\n\n${quoted}\n\n`;
    }
    case 'UL':
    case 'OL': {
      const indent = '  '.repeat(context.listDepth);
      const items = Array.from(node.childNodes).filter((child) => child.nodeName === 'LI');
      const start = Number(attr(node, 'start')) || 1;
      const lines = items.map((item, index) => {
        const marker = name === 'OL' ? `${start + index}.` : '-';
        const body = Array.from(item.childNodes)
          .map((child) => inline(child, { ...context, listDepth: context.listDepth + 1 }))
          .join('')
          .trim();
        return `${indent}${marker} ${body.replace(/\n{2,}/g, '\n')}`;
      });
      return context.listDepth > 0 ? `\n${lines.join('\n')}` : `\n\n${lines.join('\n')}\n\n`;
    }
    case 'TABLE': {
      const rows: string[][] = [];
      const collect = (current: IHtmlNode) => {
        for (const child of Array.from(current.childNodes)) {
          if (child.nodeName === 'TR') {
            rows.push(
              Array.from(child.childNodes)
                .filter((cell) => cell.nodeName === 'TD' || cell.nodeName === 'TH')
                .map((cell) => inline(cell, context).replace(/\s+/g, ' ').trim().replace(/\|/g, '\\|')),
            );
          } else if (child.nodeType === ELEMENT_NODE) collect(child);
        }
      };
      collect(node);
      if (rows.length === 0) return '';
      const width = Math.max(...rows.map((row) => row.length));
      const line = (row: string[]) =>
        `| ${Array.from({ length: width }, (_, i) => row[i] ?? '').join(' | ')} |`;
      return `\n\n${[line(rows[0]), line(Array.from({ length: width }, () => '---')), ...rows.slice(1).map(line)].join('\n')}\n\n`;
    }
    default: {
      const text = children();
      if (PARAGRAPHS.has(name)) return `\n\n${text.trim()}\n\n`;
      if (LINES.has(name)) return `\n${text}\n`;
      return text;
    }
  }
};

/**
 * A rich paste as the Markdown a person would have typed, the way ChatGPT keeps it: links, bold, lists,
 * headings, quotes, code and tables survive, the field stays plain text. Null when the copy carries no formatting:
 * the source's own plain text is then more faithful (a code editor's spans, a terminal's colours).
 */
export const htmlToMarkdown = (root: IHtmlNode): string | null => {
  if (!hasFormatting(root)) return null;
  let fenced = false;
  const lines = inline(root, { pre: false, listDepth: 0 })
    .split('\n')
    .map((line) => {
      if (line.startsWith('```')) fenced = !fenced;
      // Outside code, a line keeps list indents (two spaces a level) but not the single space collapsed markup leaves.
      return fenced ? line : line.replace(/^ (?=\S)/, '').trimEnd();
    });
  const markdown = lines
    .join('\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
  return markdown || null;
};
