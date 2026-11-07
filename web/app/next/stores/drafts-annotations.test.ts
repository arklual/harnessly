import { afterEach, describe, expect, it } from 'vitest';
import { annotationWords, MAX_ANNOTATIONS, type IAnnotation } from '@/next/features/pane/annotations';
import {
  addDraftAnnotations,
  clearDraft,
  draftAnnotations,
  draftsStore,
  editDraftAnnotation,
  emptyDraft,
  removeDraftAnnotation,
  setDraft,
  setDraftText,
} from '@/next/stores/drafts';

const k = 'tab-notes' as const;
const mark = (id: string, comment = ''): IAnnotation => ({
  id,
  comment,
  pick: 1,
  selector: `#${id}`,
  tag: 'button',
  text: 'Купить',
  rect: { x: 0, y: 0, width: 10, height: 10 },
  html: '',
  component: null,
  url: 'http://localhost:3000/shop',
});
const labels = {
  annotation: 'Аннотация',
  element: 'Элемент страницы',
  component: 'Компонент:',
  comment: 'Комментарий:',
};

afterEach(() => clearDraft(k));

describe('page notes in the draft', () => {
  it('keeps each mark as its own note, under a name of its own, and keeps them through typing', () => {
    expect(addDraftAnnotations(k, [mark('a1'), mark('a2')])).toBe(2);
    expect(addDraftAnnotations(k, [mark('a1')])).toBe(1);
    const ids = draftAnnotations(k).map((a) => a.id);
    expect(new Set(ids).size).toBe(3);
    setDraftText(k, 'hello');
    expect(draftAnnotations(k)).toHaveLength(3);
  });

  it('a draft of notes alone is not empty; a write that does not mention them leaves them', () => {
    addDraftAnnotations(k, [mark('a1')]);
    expect(emptyDraft(draftsStore.get(k)!)).toBe(false);
    setDraft(k, { text: 'x', attachments: [] });
    expect(draftAnnotations(k)).toHaveLength(1);
  });

  it('edits the comment of one note and removes one; the last one removed leaves no draft', () => {
    addDraftAnnotations(k, [mark('a1', 'old'), mark('a2')]);
    const [first, second] = draftAnnotations(k);
    editDraftAnnotation(k, first.id, 'new');
    expect(draftAnnotations(k).map((a) => a.comment)).toEqual(['new', '']);
    removeDraftAnnotation(k, first.id);
    expect(draftAnnotations(k).map((a) => a.id)).toEqual([second.id]);
    removeDraftAnnotation(k, second.id);
    expect(draftsStore.get(k)).toBeUndefined();
  });

  it('holds at most MAX_ANNOTATIONS and says how many fitted', () => {
    const many = Array.from({ length: MAX_ANNOTATIONS + 5 }, (_, i) => mark(`m${i}`));
    expect(addDraftAnnotations(k, many)).toBe(MAX_ANNOTATIONS);
    expect(addDraftAnnotations(k, [mark('x')])).toBe(0);
    expect(draftAnnotations(k)).toHaveLength(MAX_ANNOTATIONS);
  });

  it('unfolds into the words the agent gets, numbered by place and with the edited comment', () => {
    addDraftAnnotations(k, [mark('a1', 'first'), mark('a2', 'second')]);
    editDraftAnnotation(k, draftAnnotations(k)[1].id, 'changed');
    const words = annotationWords(draftAnnotations(k), (from) => from, labels);
    expect(words).toContain('Аннотация 1');
    expect(words).toContain('Аннотация 2');
    expect(words).toContain('Комментарий: first');
    expect(words).toContain('Комментарий: changed');
    expect(words).not.toContain('second');
  });

  it('goes with the draft when it is cleared (a send)', () => {
    addDraftAnnotations(k, [mark('a1')]);
    clearDraft(k);
    expect(draftAnnotations(k)).toEqual([]);
  });
});
