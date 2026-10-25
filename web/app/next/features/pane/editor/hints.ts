// Markdown hints: snippets for the blocks and marks one types often. They open on «/» (after a space or at the line's
// start) and on a block's own mark at the start of a line («#», «-», «1.», «>», «```», «|»); never inside fenced code.
import {
  snippetCompletion,
  type Completion,
  type CompletionContext,
  type CompletionResult,
} from '@codemirror/autocomplete';
import { syntaxTree } from '@codemirror/language';
import { t } from '@/next/i18n';

interface IHint {
  /** What is typed to find it: after «/» for the slash menu, the mark itself at the start of a line. */
  slash: string;
  mark: string | null;
  name: string;
  snippet: () => string;
}

const hints = (): readonly IHint[] => [
  { slash: 'h1', mark: '#', name: 'h1', snippet: () => `# \${${t('next.editor.md.title')}}` },
  { slash: 'h2', mark: '##', name: 'h2', snippet: () => `## \${${t('next.editor.md.title')}}` },
  { slash: 'h3', mark: '###', name: 'h3', snippet: () => `### \${${t('next.editor.md.title')}}` },
  { slash: 'list', mark: '-', name: 'list', snippet: () => `- \${${t('next.editor.md.item')}}` },
  { slash: 'todo', mark: '- [ ]', name: 'todo', snippet: () => `- [ ] \${${t('next.editor.md.task')}}` },
  { slash: 'num', mark: '1.', name: 'num', snippet: () => `1. \${${t('next.editor.md.item')}}` },
  { slash: 'quote', mark: '>', name: 'quote', snippet: () => `> \${${t('next.editor.md.quote')}}` },
  { slash: 'code', mark: '```', name: 'code', snippet: () => '```${}\n${}\n```' },
  {
    slash: 'table',
    mark: '|',
    name: 'table',
    snippet: () =>
      `| \${${t('next.editor.md.column')} 1} | \${${t('next.editor.md.column')} 2} |\n| --- | --- |\n| \${} | \${} |`,
  },
  {
    slash: 'link',
    mark: '[',
    name: 'link',
    snippet: () => `[\${${t('next.editor.md.text')}}](\${https://})`,
  },
  { slash: 'image', mark: '![', name: 'image', snippet: () => `![\${${t('next.editor.md.text')}}](\${})` },
  { slash: 'hr', mark: '---', name: 'hr', snippet: () => '---\n' },
  { slash: 'bold', mark: null, name: 'bold', snippet: () => `**\${${t('next.editor.md.text')}}**` },
  { slash: 'italic', mark: null, name: 'italic', snippet: () => `_\${${t('next.editor.md.text')}}_` },
  { slash: 'inline', mark: null, name: 'inline', snippet: () => '`${}`' },
];

const option = (hint: IHint, label: string): Completion =>
  snippetCompletion(hint.snippet(), { label, detail: t(`next.editor.md.hint.${hint.name}`), type: 'text' });

const SLASH = /(?:^|\s)(\/[a-z0-9]*)$/i;
const MARK = /^(\s*)(!?\[|[#>*+\-|`]{1,6}(?: \[ ?\]?)?|\d{1,3}\.?)$/;
const CODE_NODES = new Set(['FencedCode', 'CodeBlock', 'InlineCode', 'CodeText']);

const insideCode = (context: CompletionContext): boolean => {
  for (let node = syntaxTree(context.state).resolveInner(context.pos, -1); node.parent; node = node.parent)
    if (CODE_NODES.has(node.name)) return true;
  return false;
};

/** The completion source of markdown files. */
export const markdownHints = (context: CompletionContext): CompletionResult | null => {
  if (insideCode(context)) return null;
  const line = context.state.doc.lineAt(context.pos);
  const before = line.text.slice(0, context.pos - line.from);
  const slash = SLASH.exec(before);
  if (slash)
    return {
      from: context.pos - slash[1].length,
      options: hints().map((hint) => option(hint, `/${hint.slash}`)),
      validFor: /^\/[a-z0-9]*$/i,
    };
  const mark = MARK.exec(before);
  if (!mark) return null;
  return {
    from: line.from + mark[1].length,
    options: hints()
      .filter((hint) => hint.mark !== null)
      .map((hint) => option(hint, hint.mark ?? '')),
    validFor: /^(!?\[|[#>*+\-|`]{0,6}(?: \[ ?\]?)?|\d{0,3}\.?)$/,
  };
};
