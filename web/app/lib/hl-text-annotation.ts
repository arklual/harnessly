import { clip } from '@/lib/hl-preview/bridge-core';
import { t } from '@/lib/i18n';

export const MAX_QUOTE = 1200;
export const MAX_CONTEXT = 120;
const MAX_LABEL_QUOTE = 40;

/**
 * A fragment is annotated with its own comment: `note`. `ask`, `add` and `fix` are the choices of the first version,
 * kept so sent messages still read back.
 */
export const TEXT_INTENTS = ['note', 'ask', 'add', 'fix'] as const;
export type TTextIntent = (typeof TEXT_INTENTS)[number];
export const ANNOTATE: TTextIntent = 'note';

export const TEXT_ROLES = ['agent', 'user'] as const;
export type TTextRole = (typeof TEXT_ROLES)[number];

export const INTENT_LABELS: Record<TTextIntent, string> = {
  note: t('lib.hlTextAnnotation.note'),
  ask: t('lib.hlTextAnnotation.ask'),
  add: t('lib.hlTextAnnotation.add'),
  fix: t('lib.hlTextAnnotation.fix'),
};

export const INTENT_CARD_LABELS: Record<TTextIntent, string> = {
  note: t('lib.hlTextAnnotation.noteCard'),
  ask: t('lib.hlTextAnnotation.askCard'),
  add: t('lib.hlTextAnnotation.addCard'),
  fix: t('lib.hlTextAnnotation.fixCard'),
};

export const INTENT_PLACEHOLDERS: Record<TTextIntent, string> = {
  note: t('lib.hlTextAnnotation.notePlaceholder'),
  ask: t('lib.hlTextAnnotation.askPlaceholder'),
  add: t('lib.hlTextAnnotation.addPlaceholder'),
  fix: t('lib.hlTextAnnotation.fixPlaceholder'),
};

const INTENT_DESCRIPTIONS: Record<TTextIntent, string> = {
  note: 'comment on this fragment',
  ask: 'question about this fragment',
  add: 'addition to this fragment',
  fix: 'correction of this fragment',
};

export interface ITextQuote {
  quote: string;
  /** The words right before and after the fragment, one line each. */
  before: string;
  after: string;
  /** The fragment was longer than the limit and ends with «…». */
  truncated: boolean;
}

/** Where the fragment was selected: the timeline entry (a turn answer or a prompt) and whose it is. */
export interface ITextSource {
  entryId: string;
  role: TTextRole;
}

export interface INewTextAnnotation {
  kind: 'text';
  intent: TTextIntent;
  comment: string;
  quote: ITextQuote;
  source: ITextSource;
  /** The fragment in the page, painted lightly while the annotation waits in the composer; not part of what is sent. */
  range?: Range;
}

export interface ITextAnnotation extends INewTextAnnotation {
  id: string;
  n: number;
}

export interface IParsedTextQuote {
  intent: TTextIntent;
  from: TTextRole;
  text: string;
  before: string;
  after: string;
}

/**
 * The block is pasted into a terminal: an escape sequence in it (the end of a bracketed paste, then keystrokes) would be
 * read by the program as input. ESC stays visible as «␛», unicode line separators (U+2028, U+2029, U+0085) become line breaks, other control
 * characters except line break and tab go. Every field is cleaned first, before it is cut, escaped or prefixed, so that
 * nothing can be assembled from what cleaning leaves.
 */
export const stripControlChars = (text: string): string =>
  text
    .replace(/\x1b/g, '␛')
    .replace(/[\u2028\u2029\u0085]/g, '\n')
    .replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f]/g, '');

const oneLine = (text: string): string => text.replace(/\s+/g, ' ').trim();

const escapeAttribute = (text: string): string =>
  text.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] ?? c);

/** Free text stays readable; only a tag that would end the block early or open another one is broken up. */
export const keepInside = (text: string): string => text.replace(/<(\/?)(annotation)/gi, '<\\$1$2');

const contextBefore = (text: string): string => {
  const flat = oneLine(text);
  return flat.length > MAX_CONTEXT ? `…${flat.slice(-MAX_CONTEXT).trimStart()}` : flat;
};

const contextAfter = (text: string): string => {
  const flat = oneLine(text);
  return flat.length > MAX_CONTEXT ? `${flat.slice(0, MAX_CONTEXT).trimEnd()}…` : flat;
};

const normalizeFragment = (text: string): string =>
  text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .join('\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();

/** What the selection gives (its text and the text around it inside the same block) as a quote that fits the prompt; null for an empty one. */
export const buildQuote = ({
  selected,
  before,
  after,
}: {
  selected: string;
  before: string;
  after: string;
}): ITextQuote | null => {
  const fragment = normalizeFragment(stripControlChars(selected));
  if (!fragment) return null;
  const truncated = fragment.length > MAX_QUOTE;
  return {
    quote: truncated ? clip(fragment, MAX_QUOTE) : fragment,
    before: contextBefore(stripControlChars(before)),
    after: contextAfter(stripControlChars(after)),
    truncated,
  };
};

/** One annotation as the agent reads it. The quote goes line by line behind `> `, so no selected text can pass for a field of the block. */
export const formatTextAnnotation = ({ n, intent, comment, quote, source }: ITextAnnotation): string => {
  const fields = {
    comment: stripControlChars(comment).trim(),
    quote: stripControlChars(quote.quote),
    before: oneLine(stripControlChars(quote.before)),
    after: oneLine(stripControlChars(quote.after)),
    entryId: oneLine(stripControlChars(source.entryId)),
  };
  const attributes = [
    ['n', String(n)],
    ['kind', 'text'],
    ['intent', intent],
    ['from', source.role],
    ['message', fields.entryId],
  ]
    .map(([name, value]) => `${name}="${escapeAttribute(value)}"`)
    .join(' ');
  const lines = [
    `<annotation ${attributes}>`,
    `intent: ${INTENT_DESCRIPTIONS[intent]}`,
    ...(fields.before ? [`before: ${keepInside(fields.before)}`] : []),
    quote.truncated ? 'quote (cut):' : 'quote:',
    ...keepInside(fields.quote)
      .split('\n')
      .map((line) => `> ${line}`),
    ...(fields.after ? [`after: ${keepInside(fields.after)}`] : []),
    ...(fields.comment ? [`comment: ${keepInside(fields.comment)}`] : []),
    '</annotation>',
  ];
  // Fields are cleaned above; this pass finds nothing and only guards the block.
  return stripControlChars(lines.join('\n'));
};

/** The chip text, the same in the composer and in the sent message: `#1 Question · «the cache lives five minutes»`. */
export const textAnnotationLabel = ({
  n,
  intent,
  quote,
}: Pick<ITextAnnotation, 'n' | 'intent' | 'quote'>): string => {
  const short = clip(oneLine(quote.quote), MAX_LABEL_QUOTE);
  return `#${n} ${INTENT_CARD_LABELS[intent]} · «${short}»`;
};

const isIntent = (value: string | undefined): value is TTextIntent =>
  TEXT_INTENTS.some((intent) => intent === value);
const isRole = (value: string | undefined): value is TTextRole => TEXT_ROLES.some((role) => role === value);

export interface IParsedTextAnnotation {
  n: number;
  label: string;
  comment: string;
  file: null;
  quote: IParsedTextQuote;
}

/** Reads a sent text block back for the timeline; null for any other block or one that does not hold a quote. */
export const parseTextAnnotationBlock = (block: string): IParsedTextAnnotation | null => {
  const head = block.match(/^<annotation\b([^>]*)>\n([\s\S]*)\n<\/annotation>$/);
  if (!head) return null;
  const attribute = (name: string) => head[1].match(new RegExp(`\\b${name}="([^"]*)"`))?.[1];
  const n = Number(attribute('n'));
  const intent = attribute('intent');
  const from = attribute('from');
  if (attribute('kind') !== 'text' || !Number.isInteger(n) || !isIntent(intent) || !isRole(from)) return null;
  const lines = head[2].split('\n');
  const field = (at: number, prefix: string): string | null =>
    lines[at]?.startsWith(prefix) ? lines[at].slice(prefix.length) : null;
  let at = 0;
  if (field(at++, 'intent: ') === null) return null;
  const before = field(at, 'before: ');
  if (before !== null) at++;
  if (lines[at] !== 'quote:' && lines[at] !== 'quote (cut):') return null;
  const quoted: string[] = [];
  for (at++; at < lines.length && lines[at].startsWith('>'); at++) {
    quoted.push(lines[at].startsWith('> ') ? lines[at].slice(2) : lines[at].slice(1));
  }
  const after = field(at, 'after: ');
  if (after !== null) at++;
  const first = field(at, 'comment: ');
  if (first === null && at < lines.length) return null;
  const comment = first === null ? '' : [first, ...lines.slice(at + 1)].join('\n');
  const text = quoted.join('\n');
  return {
    n,
    label: textAnnotationLabel({
      n,
      intent,
      quote: { quote: text, before: '', after: '', truncated: false },
    }),
    comment,
    file: null,
    quote: { intent, from, text, before: before ?? '', after: after ?? '' },
  };
};
