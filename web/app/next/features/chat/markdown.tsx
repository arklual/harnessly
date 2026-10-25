// The agent's markdown (lazy chunk with react-markdown and remark-gfm, spec §6): paragraphs, lists, headings, code
// with a copy button, tables, and references turned into chips in place. Raw HTML is never rendered; a ```mermaid fence is
// drawn as a diagram and an ```html fence is shown in a sandboxed frame (html-doc.ts).
import type { Parent, PhrasingContent, Root, RootContent, Text } from 'mdast';
import { useContext, useState, type ReactElement, type ReactNode } from 'react';
import renderTree, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import HtmlBlock from '@/next/features/chat/html-block';
import LinkCard, { cardKindOf } from '@/next/features/chat/link-card';
import MermaidBlock from '@/next/features/chat/mermaid-block';
import { changeLinkLabel, docOfCode, splitRefs } from '@/next/features/chat/refs';
import RefChip, { RefContext } from '@/next/features/chat/ref-chip';
import type { RefKind } from '@/next/features/chat/types';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

const REF_TAG = 'hx-ref';

/** A remark step: plain text outside code and links splits into text and reference nodes. */
const remarkRefs = () => (tree: Root) => {
  const walk = (node: Parent) => {
    const next: RootContent[] = [];
    const children = node.children as RootContent[];
    for (const child of children) {
      if (child.type === 'text') {
        for (const part of splitRefs((child as Text).value)) {
          if ('text' in part) next.push({ type: 'text', value: part.text });
          else
            next.push({
              type: 'text',
              value: part.label,
              data: {
                hName: REF_TAG,
                hProperties: { dataKind: part.ref, dataLabel: part.label },
              },
            } as Text);
        }
        continue;
      }
      if ('children' in child && child.type !== 'link' && child.type !== 'linkReference')
        walk(child as Parent);
      next.push(child);
    }
    (node as Parent & { children: RootContent[] }).children = next as PhrasingContent[];
  };
  walk(tree);
};

const CODE_FOLD_LINES = 14;

const CodeBlock = ({ lang, text }: { lang: string; text: string }) => {
  const lines = text.replace(/\n$/, '').split('\n');
  const long = lines.length > CODE_FOLD_LINES;
  const [open, setOpen] = useState(!long);
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard?.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    });
  };
  const shown = open ? lines : lines.slice(0, CODE_FOLD_LINES);
  return (
    <div className="hx-code" data-collapsed={long ? String(!open) : undefined}>
      <div className="hx-code__head">
        <span>{lang || t('next.code.text')}</span>
        <span className="hx-spacer" />
        <button type="button" className="hx-code__copy" data-copied={copied || undefined} onClick={copy}>
          <Icon name={copied ? 'check' : 'copy'} size="sm" />
          {copied ? t('next.code.copied') : t('next.code.copy')}
        </button>
      </div>
      <div className="hx-code__body">
        {shown.map((line, index) => (
          <div key={index} className="hx-code__line">
            <span className="hx-code__ln">{index + 1}</span>
            <span>{line}</span>
          </div>
        ))}
      </div>
      {long && (
        <button type="button" className="hx-code__more" onClick={() => setOpen((value) => !value)}>
          <Icon name="chevron-down" size="sm" />
          {open ? t('next.code.less') : t('next.code.more', { n: lines.length - CODE_FOLD_LINES })}
        </button>
      )}
    </div>
  );
};

const textOf = (node: ReactNode): string => {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(textOf).join('');
  return '';
};

// Inline code: the path of a Markdown file the conversation touched opens like the document's card; any other code stays code.
const InlineCode = ({ children }: { children?: ReactNode }) => {
  const { docs } = useContext(RefContext);
  const text = textOf(children);
  const doc = docOfCode(text, docs);
  if (doc) return <RefChip kind="file" label={doc} text={text} />;
  return <code className="hx-md__code">{children}</code>;
};

const components = {
  [REF_TAG]: (props: { 'data-kind'?: RefKind; 'data-label'?: string }) => (
    <RefChip kind={props['data-kind'] ?? 'file'} label={props['data-label'] ?? ''} />
  ),
  // A link to a merge request, a pull request or a commit is its chip, which opens it with the diff in the pane.
  a: ({ href, children }) => {
    const change = href ? changeLinkLabel(href) : null;
    const text = textOf(children);
    if (change)
      return (
        <RefChip
          kind="mr"
          label={href ?? ''}
          href={href ?? null}
          text={text && text !== href ? text : change}
        />
      );
    return <RefChip kind="link" label={href ?? ''} href={href ?? null} text={text || href || ''} />;
  },
  pre: ({ node }) => {
    const code = node?.children[0];
    const className = code && 'properties' in code ? String(code.properties.className ?? '') : '';
    const lang = /language-([\w+-]+)/.exec(className)?.[1] ?? '';
    const value =
      code && 'children' in code ? code.children.map((c) => ('value' in c ? c.value : '')).join('') : '';
    if (lang === 'mermaid')
      return <MermaidBlock source={value} fallback={<CodeBlock lang={lang} text={value} />} />;
    if (lang === 'html') return <HtmlBlock source={value} />;
    return <CodeBlock lang={lang} text={value} />;
  },
  // A link to our own document or board, alone in its paragraph, is a card.
  p: ({ node, children }) => {
    const only = node?.children.filter((c) => c.type !== 'text' || c.value.trim() !== '');
    const link = only?.length === 1 && only[0].type === 'element' && only[0].tagName === 'a' ? only[0] : null;
    const href = link ? String(link.properties.href ?? '') : '';
    const kind = href ? cardKindOf(href) : null;
    if (kind) {
      const text = link?.children.map((c) => ('value' in c ? c.value : '')).join('') ?? '';
      return <LinkCard kind={kind} href={href} text={text} />;
    }
    return <p>{children}</p>;
  },
  code: ({ children }) => <InlineCode>{children}</InlineCode>,
  table: ({ children }) => (
    <div className="hx-md__table">
      <table>{children}</table>
    </div>
  ),
  img: () => null,
} as Components;

// A path in inline code (`/home/…/review.md`, `web/app/x.ts:12`): another agent's report names its files this way, and
// there the path opens like a chip. The agent's own text keeps its code as code.
const CODE_PATH = /^(?:~?\/)?(?:[\w@.-]+\/)+[\w@-]+(?:\.[\w-]+)+(?::\d+)?$/;
const withPaths = {
  ...components,
  code: ({ children }) => {
    const text = textOf(children);
    if (CODE_PATH.test(text)) return <RefChip kind="file" label={text} />;
    return <code className="hx-md__code">{children}</code>;
  },
} as Components;

// Parsing is the costly part of a turn: GFM (tables, strikethrough, task lists, autolinks) runs only when the text has
// a hint of it, and the result of a text is kept, so a turn that scrolls back into a long feed does not parse again.
const GFM_HINT = /\||~~|\[[ xX]\]|https?:\/\/|www\./;
const WITH_GFM = [remarkGfm, remarkRefs];
const PLAIN = [remarkRefs];
const KEPT = 400;
const parsed = new Map<string, ReactElement>();

const render = (md: string, paths: boolean): ReactElement => {
  const keyOf = paths ? `paths:${md}` : md;
  const kept = parsed.get(keyOf);
  if (kept) return kept;
  const tree = renderTree({
    children: md,
    remarkPlugins: GFM_HINT.test(md) ? WITH_GFM : PLAIN,
    components: paths ? withPaths : components,
    skipHtml: true,
  });
  if (parsed.size >= KEPT) parsed.delete(parsed.keys().next().value ?? '');
  parsed.set(keyOf, tree);
  return tree;
};

interface IMarkdownProps {
  md: string;
  /** Paths in inline code open as files (another agent's report). */
  paths?: boolean;
}

const Markdown = ({ md, paths = false }: IMarkdownProps) => <div className="hx-md">{render(md, paths)}</div>;

export default Markdown;
