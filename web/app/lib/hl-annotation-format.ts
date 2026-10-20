import { isTextAnnotation, type IAnnotation, type TComposerAnnotation } from '@/lib/hl-annotations-store';
import {
  formatTextAnnotation,
  keepInside,
  stripControlChars,
  parseTextAnnotationBlock,
  textAnnotationLabel,
  type IParsedTextQuote,
} from '@/lib/hl-text-annotation';
import { clip } from '@/lib/hl-preview/bridge-core';
import { t } from '@/lib/i18n';

const MAX_HTML = 1000;
const MAX_TEXT = 120;
const MAX_SELECTOR = 512;
const MAX_NAME = 120;
const MAX_LABEL_TEXT = 40;

const oneLine = (text: string): string => text.replace(/\s+/g, ' ').trim();

/** Cleaned first, then cut and flattened: nothing can be assembled from what cleaning leaves. */
const cleanLine = (text: string): string => oneLine(stripControlChars(text));

const escapeMarkup = (text: string): string =>
  text.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] ?? c);

/** What the page supplied, as it goes into the block: one line each, so a newline cannot forge a `file:` or `comment:` line. */
const pageFields = (element: IAnnotation['element']) => ({
  selector: clip(cleanLine(element.selector), MAX_SELECTOR),
  text: clip(cleanLine(element.text), MAX_TEXT),
  name: element.component ? clip(cleanLine(element.component.name), MAX_NAME) : null,
  hostTag: element.component?.hostTag ? clip(cleanLine(element.component.hostTag), MAX_NAME) : null,
});

const selectorTag = (selector: string): string | null =>
  selector
    .split(' > ')
    .at(-1)
    ?.match(/^[a-z][\w-]*/i)?.[0] ?? null;

/** The chip text, the same in the composer and in the sent message: `#1 BuyButtonComponent · «Buy ticket»`. */
const chipLabel = (n: number, name: string | null, selector: string, text: string): string => {
  const title = name ?? selectorTag(selector) ?? t('lib.hlAnnotationFormat.element');
  const short = clip(text, MAX_LABEL_TEXT);
  return short ? `#${n} ${title} · «${short}»` : `#${n} ${title}`;
};

/** One annotation as the agent reads it: lines without a value are left out, the page's html is escaped. */
const formatElementAnnotation = ({ n, comment, target, element, resolve }: IAnnotation): string => {
  const attributes = [
    ['n', String(n)],
    ['url', cleanLine(element.path)],
    ['viewport', `${element.viewport.w}x${element.viewport.h}`],
    ['target', 'port' in target ? `port:${target.port}` : `url:${cleanLine(target.origin)}`],
  ]
    .map(([name, value]) => `${name}="${escapeMarkup(value)}"`)
    .join(' ');
  const { selector, text, name, hostTag } = pageFields(element);
  const matches = (resolve.status === 'done' ? resolve.matches : []).map((match) => ({
    ...match,
    file: cleanLine(match.file),
  }));
  const typed = stripControlChars(comment).trim();
  const lines = [
    `<annotation ${attributes}>`,
    keepInside(text ? `element: ${selector} — «${text}»` : `element: ${selector}`),
    ...(name ? [keepInside(`component: ${name}${hostTag ? ` (${hostTag})` : ''}`)] : []),
    ...matches
      .filter((match) => match.kind === 'component')
      .map((match) => `file: ${match.file}:${match.line}`),
    ...matches
      .filter((match) => match.kind === 'template')
      .map((match) => `template: ${match.file}:${match.line}`),
    ...matches
      .filter((match) => match.kind === 'rules')
      .map((match) => `rules: ${match.file} (${t('lib.hlAnnotationFormat.rulesHint')})`),
    ...(element.outerHTML ? [`html: ${escapeMarkup(clip(cleanLine(element.outerHTML), MAX_HTML))}`] : []),
    ...(typed ? [`comment: ${keepInside(typed)}`] : []),
    '</annotation>',
  ];
  // Fields are cleaned above; this pass finds nothing and only guards the block.
  return stripControlChars(lines.join('\n'));
};

export const formatAnnotation = (annotation: TComposerAnnotation): string =>
  isTextAnnotation(annotation) ? formatTextAnnotation(annotation) : formatElementAnnotation(annotation);

/** The message that goes to the agent: the typed text, then every annotation of the tab in order. */
export const appendAnnotations = (text: string, annotations: readonly TComposerAnnotation[]): string => {
  if (!annotations.length) return text;
  const blocks = annotations.map(formatAnnotation).join('\n\n');
  return text.trim() ? `${text}\n\n${blocks}` : blocks;
};

export const annotationLabel = (annotation: TComposerAnnotation): string => {
  if (isTextAnnotation(annotation)) return textAnnotationLabel(annotation);
  const { selector, text, name } = pageFields(annotation.element);
  return chipLabel(annotation.n, name, selector, text);
};

/** A whole block as `formatAnnotation` writes it; an unclosed one is not an annotation. */
export const ANNOTATION_BLOCK_RE = /<annotation\b[^>]*>\n[\s\S]*?\n<\/annotation>/g;

export interface IParsedAnnotation {
  n: number;
  label: string;
  comment: string;
  /** The first `file:` line, null until the file was found. */
  file: string | null;
  /** Only for a fragment selected in the timeline. */
  quote?: IParsedTextQuote;
}

/** Reads a sent block back for the timeline: the chip label and the comment, as the composer showed them. */
export const parseAnnotationBlock = (block: string): IParsedAnnotation | null => {
  if (/^<annotation\b[^>]*\bkind="text"/.test(block)) return parseTextAnnotationBlock(block);
  const head = block.match(/^<annotation\b([^>]*)>\n([\s\S]*)\n<\/annotation>$/);
  const n = Number(head?.[1].match(/\bn="(\d+)"/)?.[1]);
  if (!head || !Number.isInteger(n)) return null;
  const lines = head[2].split('\n');
  const commentAt = lines.findIndex((line) => line.startsWith('comment: '));
  const fields = commentAt < 0 ? lines : lines.slice(0, commentAt);
  const value = (prefix: string) =>
    fields.find((line) => line.startsWith(prefix))?.slice(prefix.length) ?? null;
  const element = value('element: ')?.match(/^(.*?)(?: — «(.*)»)?$/);
  const component = value('component: ')?.match(/^(.*?)(?: \(([^()]*)\))?$/);
  const comment =
    commentAt < 0
      ? ''
      : [lines[commentAt].slice('comment: '.length), ...lines.slice(commentAt + 1)].join('\n');
  return {
    n,
    label: chipLabel(n, component?.[1] ?? null, element?.[1] ?? '', element?.[2] ?? ''),
    comment,
    file: value('file: '),
  };
};
