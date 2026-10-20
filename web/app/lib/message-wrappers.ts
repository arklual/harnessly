import {
  ANNOTATION_BLOCK_RE,
  parseAnnotationBlock,
  type IParsedAnnotation,
} from '@/lib/hl-annotation-format';
import { t } from '@/lib/i18n';
import { DIFF_COMMENT_BLOCK_RE, parseDiffCommentBlock, type IParsedDiffComment } from '@/lib/hl-diff-comment';

export type TUserSegment =
  | { kind: 'text'; text: string }
  | { kind: 'pasted'; text: string }
  | {
      kind: 'peer';
      name: string;
      from?: string;
      text: string;
      idleAt?: string;
      summary?: string;
      markdown?: boolean;
    }
  /** An element picked in the preview; `text` is the block as the agent got it. */
  | ({ kind: 'annotation'; text: string } & IParsedAnnotation)
  /** A comment on lines of the diff; `text` is the block as the agent got it. */
  | ({ kind: 'diff-comment'; text: string } & IParsedDiffComment);

const CC_MEMORY_TAG = /<\/?cc-memory\b[^>]*>/g;
// An unterminated wrapper runs to the end of the prompt: Claude Code truncates long pastes without closing them.
// `<agent-message>` is what a background agent of this session sends it with SendMessage.
const WRAPPER_RE =
  /<pasted_content\b[^>]*>([\s\S]*?)(?:<\/pasted_content\b[^>]*>|$)|<cross-session-message\b([^>]*)>([\s\S]*?)(?:<\/cross-session-message>|$)|<teammate-message\b((?:"[^"]*"|[^>"])*)>([\s\S]*?)(?:<\/teammate-message>|$)|<agent-message\b([^>]*)>([\s\S]*?)(?:<\/agent-message>|$)/g;

// Claude Code frames a teammate's message for the model; none of it is the owner's.
const TEAMMATE_FRAMING_RE =
  /^Another Claude session sent a message:[ \t]*\n?|^This came from another Claude session\b[^\n]*$/gm;
const TEAMMATE_NOTICE_START = 'Another Claude session sent a message:';

// Claude Code's harness tells a session that a peer it watches went idle; the person never typed it.
const IDLE_NOTICE_RE = /\[Cross-session idle notice\]\s*"([^"]+)"[\s\S]*?(?:calls for it\.|$)/g;

const attr = (attrs: string, name: string) => attrs.match(new RegExp(`\\b${name}="([^"]*)"`))?.[1];

/** An idle notification is JSON around the teammate's final report; any other body is shown as it came. */
const teammateBody = (raw: string): Pick<Extract<TUserSegment, { kind: 'peer' }>, 'text' | 'markdown'> => {
  const body = clean(raw);
  if (!body.startsWith('{')) return { text: body };
  try {
    const data = JSON.parse(body) as { type?: unknown; result?: unknown };
    if (data.type !== 'idle_notification') return { text: body };
    const result = typeof data.result === 'string' ? clean(data.result) : '';
    return result ? { text: result, markdown: true } : { text: '' };
  } catch {
    return { text: body };
  }
};

export const stripMemoryTags = (text: string) => text.replace(CC_MEMORY_TAG, '');

const clean = (text: string) => stripMemoryTags(text).replace(/^\n+|\s+$/g, '');

/**
 * Splits a prompt into what the owner typed, what they pasted, the preview elements they attached and what another
 * agent session sent or reported.
 */
export const parseUserSegments = (input: string): TUserSegment[] => {
  const text = input.includes('<teammate-message') ? input.replace(TEAMMATE_FRAMING_RE, '') : input;
  const segments: TUserSegment[] = [];
  const pushText = (kind: 'text' | 'pasted', chunk: string) => {
    const value = clean(chunk);
    if (value.trim()) segments.push({ kind, text: value });
  };
  const pushPlain = (kind: 'text' | 'pasted', chunk: string) => {
    const blocks = [
      ...[...chunk.matchAll(ANNOTATION_BLOCK_RE)].flatMap((match) => {
        const parsed = parseAnnotationBlock(match[0]);
        return parsed
          ? [
              {
                at: match.index,
                length: match[0].length,
                segment: { kind: 'annotation' as const, ...parsed, text: match[0] },
              },
            ]
          : [];
      }),
      ...[...chunk.matchAll(DIFF_COMMENT_BLOCK_RE)].flatMap((match) => {
        const parsed = parseDiffCommentBlock(match[0]);
        return parsed
          ? [
              {
                at: match.index,
                length: match[0].length,
                segment: { kind: 'diff-comment' as const, ...parsed, text: match[0] },
              },
            ]
          : [];
      }),
    ].sort((a, b) => a.at - b.at);
    let last = 0;
    for (const block of blocks) {
      if (block.at < last) continue;
      pushText(kind, chunk.slice(last, block.at));
      last = block.at + block.length;
      segments.push(block.segment);
    }
    pushText(kind, chunk.slice(last));
  };
  const pushChunk = (kind: 'text' | 'pasted', chunk: string) => {
    let last = 0;
    for (const match of chunk.matchAll(IDLE_NOTICE_RE)) {
      pushPlain(kind, chunk.slice(last, match.index));
      last = match.index + match[0].length;
      const idleAt = match[0].match(/finished a turn at (\d{1,2}:\d{2})/)?.[1];
      const report = match[0].match(/reports:\s*«([^»]*)»/)?.[1];
      segments.push({
        kind: 'peer',
        name: match[1],
        text: clean(report ?? ''),
        ...(idleAt ? { idleAt } : {}),
      });
    }
    pushPlain(kind, chunk.slice(last));
  };
  let last = 0;
  for (const match of text.matchAll(WRAPPER_RE)) {
    pushChunk('text', text.slice(last, match.index));
    last = match.index + match[0].length;
    if (match[1] !== undefined) {
      pushChunk('pasted', match[1]);
    } else if (match[6] !== undefined) {
      const from = attr(match[6], 'from');
      segments.push({
        kind: 'peer',
        name: t('lib.messageWrappers.backgroundAgent'),
        ...(from ? { from } : {}),
        text: clean(match[7] ?? ''),
        markdown: true,
      });
    } else if (match[4] !== undefined) {
      const summary = attr(match[4], 'summary');
      segments.push({
        kind: 'peer',
        name: attr(match[4], 'teammate_id') ?? t('lib.messageWrappers.otherSession'),
        ...(summary ? { summary } : {}),
        ...teammateBody(match[5] ?? ''),
      });
    } else {
      const attrs = match[2] ?? '';
      const from = attr(attrs, 'from');
      segments.push({
        kind: 'peer',
        name: attr(attrs, 'from-name') ?? from ?? t('lib.messageWrappers.otherSession'),
        ...(from ? { from } : {}),
        text: clean(match[3] ?? ''),
      });
    }
  }
  pushChunk('text', text.slice(last));
  return segments;
};

export const isHarnessNotice = (text: string) => {
  const head = text.trimStart();
  return (
    head.startsWith('[Cross-session idle notice]') ||
    head.startsWith('[Cross-session delivery notice]') ||
    head.startsWith(TEAMMATE_NOTICE_START)
  );
};

export const isPeerOnly = (segments: TUserSegment[]) =>
  segments.length > 0 && segments.every((segment) => segment.kind === 'peer');

const segmentPlainText = (segment: TUserSegment) =>
  segment.kind === 'annotation' || segment.kind === 'diff-comment'
    ? `${segment.label} ${segment.comment}`
    : segment.text;

/** One line of plain text for lists and titles: wrappers dropped, pasted and peer text kept, an annotation as its chip and comment. */
export const plainUserText = (text: string) =>
  parseUserSegments(text).map(segmentPlainText).join(' ').replace(/\s+/g, ' ').trim();
