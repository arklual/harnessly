// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import {
  cleanDeckHtml,
  postDeck,
  readDeckMessage,
  readPickedPoint,
} from '@/next/features/designly/deck/bridge';

describe('readDeckMessage', () => {
  it('reads what the board says when it is ready', () => {
    expect(readDeckMessage({ type: 'tk:deck-ready', w: 1920, h: 1080, steps: 3 })).toEqual({
      k: 'ready',
      w: 1920,
      h: 1080,
      steps: 3,
    });
    expect(readDeckMessage({ type: 'tk:deck-ready', w: 0, h: 1080 })).toBeNull();
  });

  it('reads a hover, and a hover over nothing', () => {
    expect(
      readDeckMessage({
        type: 'tk:deck-hover',
        tpl: 12,
        name: 'Список',
        rect: [10, 20, 300, 200],
        moved: true,
        movable: true,
        was: 'h2:' + 'x'.repeat(100),
        px: true,
        tx: 40,
        ty: -12.5,
      }),
    ).toEqual({
      k: 'hover',
      tpl: 12,
      name: 'Список',
      rect: [10, 20, 300, 200],
      moved: true,
      movable: true,
      was: ('h2:' + 'x'.repeat(100)).slice(0, 64),
      px: true,
      tx: 40,
      ty: -12.5,
    });
    expect(readDeckMessage({ type: 'tk:deck-hover', tpl: null, rect: [1, 2, 3, 4] })).toEqual({
      k: 'hover',
      tpl: null,
      name: '',
      rect: null,
      moved: false,
      movable: false,
      was: '',
      px: false,
      tx: 0,
      ty: 0,
    });
  });

  it('reads a selection and refuses one without a box', () => {
    expect(
      readDeckMessage({
        type: 'tk:deck-select',
        tpl: 4,
        label: 'Заголовок',
        kind: 'title',
        size: 44,
        rect: [0, 0, 800, 90],
        was: 'h1:1x2y3z',
      }),
    ).toMatchObject({
      k: 'select',
      kind: 'title',
      size: 44,
      was: 'h1:1x2y3z',
    });
    expect(readDeckMessage({ type: 'tk:deck-select', tpl: 4, rect: [0, 0] })).toBeNull();
    expect(readDeckMessage({ type: 'tk:deck-select', tpl: -1, rect: [0, 0, 1, 1] })).toBeNull();
  });

  it('cleans the markup of an edited text', () => {
    const message = readDeckMessage({
      type: 'tk:deck-text',
      tpl: 7,
      was: 'Как агент ходит',
      html: '<B onclick="x()">Как</B> агент <script>alert(1)</script><span style="color:red">ходит</span><br/>',
    });
    expect(message).toEqual({
      k: 'text',
      tpl: 7,
      was: 'Как агент ходит',
      html: '<b>Как</b> агент alert(1)ходит<br>',
    });
  });

  it('reads a drag with its guides', () => {
    expect(
      readDeckMessage({
        type: 'tk:deck-drag',
        tpl: 3,
        x: 120,
        y: -8,
        guides: [{ v: 1, at: 960 }, { v: 2, at: 1 }, 'x'],
        end: true,
        was: 'figure:abc',
        px: 'yes',
      }),
    ).toEqual({
      k: 'drag',
      tpl: 3,
      x: 120,
      y: -8,
      guides: [{ v: 1, at: 960 }],
      end: true,
      was: 'figure:abc',
      px: false,
    });
  });

  it('reads a hit answer, a pointer and a tap', () => {
    expect(readDeckMessage({ type: 'tk:deck-hit', req: 2, label: 'Схема', tpl: 9 })).toEqual({
      k: 'hit',
      req: 2,
      label: 'Схема',
      tpl: 9,
    });
    expect(readDeckMessage({ type: 'tk:deck-pointer', x: 1, y: 2, down: true })).toEqual({
      k: 'pointer',
      x: 1,
      y: 2,
      down: true,
    });
    expect(readDeckMessage({ type: 'tk:deck-tap', x: 1, y: 2, text: 'Hi' })).toEqual({
      k: 'tap',
      x: 1,
      y: 2,
      text: 'Hi',
    });
    expect(readDeckMessage({ type: 'tk:deck-press', x: 10, y: 20, phase: 'ring' })).toEqual({
      k: 'press',
      x: 10,
      y: 20,
      phase: 'ring',
    });
    expect(readDeckMessage({ type: 'tk:picked', selector: 'p', tag: 'p', point: [30, 40] })).toEqual({
      k: 'held',
      x: 30,
      y: 40,
    });
  });

  it('ignores what is not a deck message', () => {
    expect(readDeckMessage({ type: 'tk:picked' })).toBeNull();
    expect(readDeckMessage({ type: 'tk:deck-unknown' })).toBeNull();
    expect(readDeckMessage(null)).toBeNull();
    expect(readDeckMessage({ type: 'tk:deck-pointer', x: Infinity, y: 0 })).toBeNull();
    expect(readDeckMessage({ type: 'tk:deck-press', x: 1, y: 2, phase: 'hold' })).toBeNull();
  });
});

describe('the rest of the bridge', () => {
  it('reads the point of a pick', () => {
    expect(readPickedPoint({ type: 'tk:picked', point: [40, 72] })).toEqual([40, 72]);
    expect(readPickedPoint({ type: 'tk:picked' })).toBeNull();
  });

  it('keeps only the inline tags of an edit', () => {
    expect(cleanDeckHtml('<em>a</em><div>b</div><u>c</u>')).toBe('<em>a</em>b<u>c</u>');
  });

  it('posts to the frame’s own origin only', () => {
    const post = vi.fn();
    const frame = { contentWindow: { postMessage: post } } as unknown as HTMLIFrameElement;
    postDeck(frame, 'http://127.0.0.1:4100/a/acme/p/Cover.dc.html', { type: 'tk:deck-step', n: 2 });
    expect(post).toHaveBeenCalledWith({ type: 'tk:deck-step', n: 2 }, 'http://127.0.0.1:4100');
    postDeck(null, 'http://127.0.0.1:4100/', { type: 'tk:deck-step', n: 2 });
    expect(post).toHaveBeenCalledTimes(1);
  });
});
