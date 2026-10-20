import { oneLineSafe, stripControlChars } from '@/lib/terminal-safe-text';
import { clip } from '@/lib/hl-preview/bridge-core';
import { plural, t } from '@/lib/i18n';

const MAX_LINES = 80;
const MAX_LINE = 400;
const HUNK_HEADER = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;
const MARKER = { add: '+', del: '-', ctx: ' ' } as const;

export type TDiffLineKind = keyof typeof MARKER;

export interface IDiffLine {
  kind: TDiffLineKind;
  oldNo: number | null;
  newNo: number | null;
  text: string;
}

export interface IDiffComment {
  id: string;
  /** Order within the tab, from 1 again after a clear. */
  n: number;
  file: string;
  repo: string | null;
  /** Short sha when the diff is a commit's; null for uncommitted changes. */
  commit: string | null;
  lines: readonly IDiffLine[];
  comment: string;
}

export interface IDiffSpan {
  side: 'new' | 'old';
  from: number;
  to: number;
}

/** Lines of the hunks of one file with their numbers on both sides, as `git diff` counts them. */
export const parseHunkLines = (hunks: readonly string[]): IDiffLine[] =>
  hunks.flatMap((hunk) => {
    const [header, ...body] = hunk.split('\n');
    const start = header.match(HUNK_HEADER);
    if (!start) return [];
    let oldNo = Number(start[1]);
    let newNo = Number(start[2]);
    const parsed: IDiffLine[] = [];
    for (const raw of body) {
      if (raw === '' || raw.startsWith('\\')) continue;
      const text = raw.slice(1);
      if (raw.startsWith('+')) parsed.push({ kind: 'add', oldNo: null, newNo: newNo++, text });
      else if (raw.startsWith('-')) parsed.push({ kind: 'del', oldNo: oldNo++, newNo: null, text });
      else if (raw.startsWith(' ')) parsed.push({ kind: 'ctx', oldNo: oldNo++, newNo: newNo++, text });
    }
    return parsed;
  });

export const lineKey = ({ oldNo, newNo }: Pick<IDiffLine, 'oldNo' | 'newNo'>): string =>
  `${oldNo ?? ''}:${newNo ?? ''}`;

/** Both clicked rows and everything between them, in diff order; empty when either row is not in the diff. */
export const rangeBetween = (lines: readonly IDiffLine[], fromKey: string, toKey: string): IDiffLine[] => {
  const from = lines.findIndex((line) => lineKey(line) === fromKey);
  const to = lines.findIndex((line) => lineKey(line) === toKey);
  if (from < 0 || to < 0) return [];
  return lines.slice(Math.min(from, to), Math.max(from, to) + 1);
};

/** The range the agent can find in the file: new numbers, or the old ones when only deleted lines are selected. */
export const diffSpan = (lines: readonly IDiffLine[]): IDiffSpan | null => {
  const added = lines.flatMap((line) => (line.newNo === null ? [] : [line.newNo]));
  if (added.length) return { side: 'new', from: Math.min(...added), to: Math.max(...added) };
  const removed = lines.flatMap((line) => (line.oldNo === null ? [] : [line.oldNo]));
  return removed.length ? { side: 'old', from: Math.min(...removed), to: Math.max(...removed) } : null;
};

const spanText = ({ from, to }: IDiffSpan): string => (from === to ? String(from) : `${from}-${to}`);

const escapeMarkup = (text: string): string =>
  text.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] ?? c);

const unescapeMarkup = (text: string): string =>
  text.replace(
    /&(quot|gt|lt|amp);/g,
    (_, name: string) => ({ quot: '"', gt: '>', lt: '<', amp: '&' })[name] ?? '',
  );

/** Free text stays readable; only a closing tag that would end the block early is broken up. */
const keepInside = (text: string): string => text.replace(/<\/(diff-comment)/gi, '<\\/$1');

const fenceFor = (fragment: string): string => {
  const longest = Math.max(0, ...(fragment.match(/`+/g) ?? []).map((run) => run.length));
  return '`'.repeat(Math.max(3, longest + 1));
};

const labelOf = (file: string, range: string | null, old: boolean): string => {
  const name = file.split('/').at(-1) ?? file;
  return range ? (old ? t('lib.hlDiffComment.oldRange', { name, range }) : `${name}:${range}`) : name;
};

/** The chip text, the same in the composer and in the sent message: `changes-tab.tsx:12-15`. */
export const diffCommentLabel = ({ file, lines }: Pick<IDiffComment, 'file' | 'lines'>): string => {
  const span = diffSpan(lines);
  return labelOf(file, span && spanText(span), span?.side === 'old');
};

/** One comment as the agent reads it: where, the diff fragment as `git diff` shows it, then what the person wants. */
export const formatDiffComment = ({ n, file, repo, commit, lines, comment }: IDiffComment): string => {
  const span = diffSpan(lines);
  const clean = stripControlChars(comment);
  const attributes = [
    ['n', String(n)],
    ['file', file],
    ...(span
      ? [
          ['lines', spanText(span)],
          ['side', span.side],
        ]
      : []),
    ...(repo ? [['repo', repo]] : []),
    ...(commit ? [['commit', commit]] : []),
  ]
    .map(([name, value]) => `${name}="${escapeMarkup(oneLineSafe(value))}"`)
    .join(' ');
  const shown = lines
    .slice(0, MAX_LINES)
    .map((line) => keepInside(`${MARKER[line.kind]}${clip(stripControlChars(line.text), MAX_LINE)}`));
  const hidden = lines.length - shown.length;
  const fence = fenceFor(shown.join('\n'));
  return [
    `<diff-comment ${attributes}>`,
    `${fence}diff`,
    ...shown,
    fence,
    ...(hidden > 0 ? [`… ${plural(hidden, 'lib.hlDiffComment.moreLines')}`] : []),
    ...(clean.trim() ? [`comment: ${keepInside(clean.trim())}`] : []),
    '</diff-comment>',
  ].join('\n');
};

/** The message that goes to the agent: the typed text, then every diff comment of the tab in order. */
export const appendDiffComments = (text: string, comments: readonly IDiffComment[]): string => {
  if (!comments.length) return text;
  const blocks = comments.map(formatDiffComment).join('\n\n');
  return text.trim() ? `${text}\n\n${blocks}` : blocks;
};

/** A whole block as `formatDiffComment` writes it; an unclosed one is not a comment. */
export const DIFF_COMMENT_BLOCK_RE = /<diff-comment\b[^>]*>\n[\s\S]*?\n<\/diff-comment>/g;

export interface IParsedDiffComment {
  n: number;
  label: string;
  file: string;
  comment: string;
}

/** Reads a sent block back for the timeline: the chip label and the comment, as the composer showed them. */
export const parseDiffCommentBlock = (block: string): IParsedDiffComment | null => {
  const head = block.match(/^<diff-comment\b([^>]*)>\n([\s\S]*)\n<\/diff-comment>$/);
  if (!head) return null;
  const attribute = (name: string) => {
    const value = head[1].match(new RegExp(`\\b${name}="([^"]*)"`))?.[1];
    return value === undefined ? null : unescapeMarkup(value);
  };
  const n = Number(attribute('n'));
  const file = attribute('file');
  if (!Number.isInteger(n) || file === null) return null;
  const body = head[2].split('\n');
  const commentAt = body.findIndex((line) => line.startsWith('comment: '));
  const comment =
    commentAt < 0 ? '' : [body[commentAt].slice('comment: '.length), ...body.slice(commentAt + 1)].join('\n');
  return { n, file, label: labelOf(file, attribute('lines'), attribute('side') === 'old'), comment };
};
