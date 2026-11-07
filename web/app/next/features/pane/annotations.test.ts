import { describe, expect, it } from 'vitest';
import {
  MAX_ANNOTATIONS,
  addAnnotation,
  annotationAnchors,
  annotationName,
  annotationWords,
  editAnnotation,
  numberOf,
  removeAnnotation,
} from '@/next/features/pane/annotations';
import type { IFramePicked } from '@/next/features/pane/frame-bridge';

const picked = (selector: string, over: Partial<IFramePicked> = {}): IFramePicked => ({
  pick: 7,
  selector,
  tag: 'button',
  text: 'Купить',
  rect: { x: 1, y: 2, width: 30, height: 10 },
  html: '<button>Купить</button>',
  component: null,
  url: 'http://localhost:3000/shop',
  ...over,
});

const labels = {
  annotation: 'Аннотация',
  element: 'Элемент страницы',
  component: 'Компонент:',
  comment: 'Комментарий:',
};

describe('annotations', () => {
  it('numbers marks by their place and closes the gap after a removal', () => {
    let list = addAnnotation([], picked('#a'));
    list = addAnnotation(list, picked('#b'));
    list = addAnnotation(list, picked('#c'));
    const [a, b, c] = list;
    expect([a, b, c].map((x) => numberOf(list, x.id))).toEqual([1, 2, 3]);
    list = removeAnnotation(list, b.id);
    expect(numberOf(list, c.id)).toBe(2);
    expect(numberOf(list, b.id)).toBe(0);
  });

  it('edits only the comment of one mark', () => {
    const list = addAnnotation(addAnnotation([], picked('#a')), picked('#b'));
    const next = editAnnotation(list, list[1].id, 'Сделать крупнее');
    expect(next.map((x) => x.comment)).toEqual(['', 'Сделать крупнее']);
    expect(next[1].selector).toBe('#b');
    expect(list[1].comment).toBe('');
  });

  it('keeps ids unique and does not add past the limit', () => {
    let list: ReturnType<typeof addAnnotation> = [];
    for (let i = 0; i < MAX_ANNOTATIONS + 5; i += 1) list = addAnnotation(list, picked(`#e${i}`));
    expect(list).toHaveLength(MAX_ANNOTATIONS);
    expect(new Set(list.map((x) => x.id)).size).toBe(MAX_ANNOTATIONS);
  });

  it('tells the bridge which blocks to follow', () => {
    const list = addAnnotation(addAnnotation([], picked('#a')), picked('#b', { pick: null }));
    expect(annotationAnchors(list)).toEqual([
      { key: list[0].id, pick: 7, selector: '#a' },
      { key: list[1].id, pick: null, selector: '#b' },
    ]);
  });

  it('writes one numbered block per mark for the agent', () => {
    let list = addAnnotation([], picked('#a'), 'Цвет не тот');
    list = addAnnotation(list, picked('#b', { text: '', html: '' }));
    const words = annotationWords(list, (from) => from, labels);
    expect(words).toContain('Аннотация 1\nЭлемент страницы http://localhost:3000/shop');
    expect(words).toContain('Комментарий: Цвет не тот');
    expect(words).toContain('Аннотация 2');
    expect(words.indexOf('Аннотация 1')).toBeLessThan(words.indexOf('Аннотация 2'));
    expect(words.split('Комментарий:')).toHaveLength(2);
  });

  it('names a mark by its text, else its tag', () => {
    expect(annotationName(addAnnotation([], picked('#a'))[0])).toBe('«Купить»');
    expect(annotationName(addAnnotation([], picked('#a', { text: '' }))[0])).toBe('<button>');
  });
});
