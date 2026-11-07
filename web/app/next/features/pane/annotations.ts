// The annotations of a browser tab: the numbered marks a person puts on the page's elements, each with a comment. The
// list lives in the tab; a mark's number is its place in the list, so a removed mark closes the gap. What goes to the
// chat is one block of words per mark (pickedWords, numbered), and the page's bridge follows each mark's element
// (`tk:anchors`) by the id the mark keeps.
import { pickedWords, type IFramePicked, type IPickedLabels } from '@/next/features/pane/frame-bridge';
import type { IAnchorItem } from '@/next/features/designly/note-anchor';

export interface IAnnotation extends IFramePicked {
  /** Kept through renumbering: the name the bridge follows the element by. */
  id: string;
  comment: string;
}

/** The most marks a page holds: the bridge follows each one, and the chat's field takes their words. */
export const MAX_ANNOTATIONS = 30;

let seq = 0;
const nextId = (): string => {
  seq += 1;
  return `a${seq}`;
};

/** Puts one more mark at the end; at the limit the list stays as it is. */
export const addAnnotation = (
  list: readonly IAnnotation[],
  picked: IFramePicked,
  comment = '',
): IAnnotation[] =>
  list.length >= MAX_ANNOTATIONS ? [...list] : [...list, { ...picked, id: nextId(), comment }];

/**
 * The marks of a send, put after the notes a draft already holds: each gets a name of its own (the page's marks start
 * from a1 again on every reload), at most MAX_ANNOTATIONS in all. `added` is how many fitted.
 */
export const appendAnnotations = (
  have: readonly IAnnotation[],
  marks: readonly IAnnotation[],
): { list: IAnnotation[]; added: number } => {
  const room = Math.max(0, MAX_ANNOTATIONS - have.length);
  const take = marks.slice(0, room).map((a) => ({ ...a, id: draftNoteId() }));
  return { list: [...have, ...take], added: take.length };
};

let noteSeq = 0;
/** A name for a note in a draft: unique across reloads, so a note sent later never meets an older one by name. */
const draftNoteId = (): string => {
  noteSeq += 1;
  return `n${Date.now().toString(36)}${noteSeq}`;
};

export const editAnnotation = (list: readonly IAnnotation[], id: string, comment: string): IAnnotation[] =>
  list.map((a) => (a.id === id ? { ...a, comment } : a));

export const removeAnnotation = (list: readonly IAnnotation[], id: string): IAnnotation[] =>
  list.filter((a) => a.id !== id);

/** The number a mark shows: 1, 2, 3… by its place in the list; 0 for a mark that is not there. */
export const numberOf = (list: readonly IAnnotation[], id: string): number =>
  list.findIndex((a) => a.id === id) + 1;

/** The blocks the bridge follows for the marks. */
export const annotationAnchors = (list: readonly IAnnotation[]): IAnchorItem[] =>
  list.map((a) => ({ key: a.id, pick: a.pick, selector: a.selector }));

export interface IAnnotationLabels extends IPickedLabels {
  /** «Аннотация» → «Аннотация 2». */
  annotation: string;
}

/** What the agent is told: each mark under its number, with the element, where it is written and the comment. */
export const annotationWords = (
  list: readonly IAnnotation[],
  pageUrl: (from: string) => string,
  labels: IAnnotationLabels,
): string =>
  list
    .map((a, i) => `${labels.annotation} ${i + 1}\n${pickedWords(a, pageUrl(a.url), a.comment, labels)}`)
    .join('\n\n');

/** A mark's name in a list: its element's text, else its tag. */
export const annotationName = (a: IAnnotation): string => (a.text ? `«${a.text}»` : `<${a.tag}>`);
