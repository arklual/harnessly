import { describe, expect, it } from 'vitest';
import { pickedLabel, readBridgeMessage } from '@/next/features/designly/page-bridge';

const picked = {
  type: 'tk:picked',
  selector: 'main > h1',
  tag: 'h1',
  text: 'Куда сходить',
  rect: { x: 10, y: 20, width: 300, height: 40 },
  component: { name: 'Hero' },
  outerHTML: '<h1>…</h1>',
  pick: 3,
  look: { background: '#AABBCC', radius: 12, opacity: 0.5 },
};

describe('readBridgeMessage', () => {
  it('takes only the keys a guest page knows', () => {
    expect(readBridgeMessage({ type: 'tk:key', key: 'f' })).toEqual({ k: 'key', key: 'f' });
    expect(readBridgeMessage({ type: 'tk:key', key: 'Escape' })).toEqual({ k: 'key', key: 'Escape' });
    expect(readBridgeMessage({ type: 'tk:key', key: 'Enter' })).toBeNull();
  });
  it('reads a pick with only the fields the design uses', () => {
    expect(readBridgeMessage(picked)).toEqual({
      k: 'picked',
      picked: {
        id: 3,
        look: { background: '#aabbcc', radius: 12, opacity: 0.5 },
        selector: 'main > h1',
        tag: 'h1',
        text: 'Куда сходить',
        rect: { x: 10, y: 20, width: 300, height: 40 },
        component: 'Hero',
        html: '<h1>…</h1>',
      },
    });
  });

  it('reads a pick from a bridge that does not number or read blocks as one without looks', () => {
    const { pick: _pick, look: _look, ...old } = picked;
    const read = readBridgeMessage(old);
    expect(read?.k === 'picked' && [read.picked.id, read.picked.look]).toEqual([null, null]);
  });

  it('keeps only a look the card can show', () => {
    const look = (value: unknown) => {
      const read = readBridgeMessage({ ...picked, look: value });
      return read?.k === 'picked' ? read.picked.look : 'dropped';
    };
    expect(look({ background: 'red', radius: -1, opacity: 3 })).toEqual({
      background: null,
      radius: null,
      opacity: 1,
    });
    expect(look({ background: null, radius: null })).toBeNull();
    expect(look('x')).toBeNull();
    expect(readBridgeMessage({ ...picked, pick: 0 })).toMatchObject({ picked: { id: null } });
  });

  it('reads which blocks the page no longer has', () => {
    expect(readBridgeMessage({ type: 'tk:styled', gone: [2, 'x', -1, 5] })).toEqual({
      k: 'styled',
      gone: [2, 5],
    });
    expect(readBridgeMessage({ type: 'tk:styled' })).toBeNull();
  });

  it('reads hello and the end of a pick', () => {
    expect(readBridgeMessage({ type: 'tk:hello', nonce: 'x' })).toEqual({ k: 'hello' });
    expect(readBridgeMessage({ type: 'tk:pick', on: false })).toEqual({ k: 'pick-off' });
    expect(readBridgeMessage({ type: 'tk:pick', on: true })).toBeNull();
  });

  it('reads how far the page is on its way', () => {
    expect(readBridgeMessage({ type: 'tk:progress', state: 'interactive', resources: 7 })).toEqual({
      k: 'progress',
      state: 'interactive',
      resources: 7,
    });
    expect(readBridgeMessage({ type: 'tk:progress', state: 'loading', resources: -2.5 })).toEqual({
      k: 'progress',
      state: 'loading',
      resources: 0,
    });
    expect(readBridgeMessage({ type: 'tk:progress', state: 'sleeping', resources: 1 })).toBeNull();
    expect(readBridgeMessage({ type: 'tk:progress', state: 'loading', resources: 'many' })).toBeNull();
  });

  it('reads where the page was scrolled to', () => {
    expect(readBridgeMessage({ type: 'tk:scroll', y: 240 })).toEqual({ k: 'scroll', y: 240 });
    expect(readBridgeMessage({ type: 'tk:scroll', y: -3 })).toEqual({ k: 'scroll', y: 0 });
    expect(readBridgeMessage({ type: 'tk:wheel', x: 10, y: 20, dy: -8 })).toEqual({
      k: 'wheel',
      x: 10,
      y: 20,
      dy: -8,
    });
    expect(readBridgeMessage({ type: 'tk:wheel', x: 1, y: 1, dy: 1e9 })).toEqual({
      k: 'wheel',
      x: 1,
      y: 1,
      dy: 2000,
    });
    expect(readBridgeMessage({ type: 'tk:wheel', x: 'a', y: 1, dy: 1 })).toBeNull();
    expect(readBridgeMessage({ type: 'tk:space', down: true })).toEqual({ k: 'space', down: true });
    expect(readBridgeMessage({ type: 'tk:space', down: 1 })).toBeNull();
    expect(readBridgeMessage({ type: 'tk:pan', dx: -3, dy: 1e9, by: 'drag' })).toEqual({
      k: 'pan',
      dx: -3,
      dy: 2000,
      drag: true,
    });
    expect(readBridgeMessage({ type: 'tk:pan', dx: 1, dy: 2, by: 'wheel' })).toEqual({
      k: 'pan',
      dx: 1,
      dy: 2,
      drag: false,
    });
    expect(readBridgeMessage({ type: 'tk:pan', dx: '1', dy: 2 })).toBeNull();
    expect(readBridgeMessage({ type: 'tk:scroll', y: '240' })).toBeNull();
    expect(readBridgeMessage({ type: 'tk:scroll' })).toBeNull();
  });

  it('drops what is not the bridge or is malformed', () => {
    expect(readBridgeMessage(null)).toBeNull();
    expect(readBridgeMessage('tk:picked')).toBeNull();
    expect(readBridgeMessage({ ...picked, rect: { x: '1', y: 2, width: 3, height: 4 } })).toBeNull();
    expect(readBridgeMessage({ ...picked, selector: 42 })).toBeNull();
  });

  it('clips the page words', () => {
    const long = readBridgeMessage({ ...picked, text: 'а'.repeat(500), selector: 's'.repeat(900) });
    expect(long?.k === 'picked' && long.picked.text.length).toBe(120);
    expect(long?.k === 'picked' && long.picked.selector.length).toBe(512);
  });
});

describe('pickedLabel', () => {
  const base = {
    id: null,
    look: null,
    selector: 'b',
    tag: 'span',
    rect: { x: 0, y: 0, width: 1, height: 1 },
    html: '',
  };
  it('prefers the words, then a meaningful component, then the tag', () => {
    expect(pickedLabel({ ...base, text: 'Билеты', component: 'Tickets' })).toBe('Билеты');
    expect(pickedLabel({ ...base, text: '', component: 'Tickets' })).toBe('Tickets');
    expect(pickedLabel({ ...base, text: '', component: 'c_' })).toBe('<span>');
  });
});

describe('readBridgeMessage: settings of a board', () => {
  const control = {
    name: 'theme',
    label: 'Theme',
    section: 'Screen',
    editor: 'enum',
    value: 'dark',
    options: [
      { value: 'dark', text: 'Dark' },
      { value: 'light', text: 'Light' },
    ],
    min: null,
    max: null,
    step: null,
  };

  it('reads what the board offers and drops what is no setting', () => {
    const read = readBridgeMessage({
      type: 'tk:tweaks',
      controls: [control, null, { name: '' }, 'x', { label: 'no name' }],
    });
    expect(read).toEqual({ k: 'tweaks', controls: [control] });
  });

  it('keeps plain values only and bounds the lists and the texts', () => {
    const read = readBridgeMessage({
      type: 'tk:tweaks',
      controls: [
        {
          ...control,
          value: { not: 'plain' },
          options: [
            { value: 'a', text: 'A' },
            { value: { x: 1 }, text: 'B' },
            { value: 3, text: 'C' },
          ],
          label: 'L'.repeat(500),
        },
      ],
    });
    expect(read?.k).toBe('tweaks');
    if (read?.k !== 'tweaks') return;
    expect(read.controls[0].value).toBeNull();
    expect(read.controls[0].options).toEqual([
      { value: 'a', text: 'A' },
      { value: 3, text: 'C' },
    ]);
    expect(read.controls[0].label).toHaveLength(120);
  });

  it('reads the library script hello and the page wish to show or hide the panel', () => {
    expect(readBridgeMessage({ type: 'tk:lib-ready' })).toEqual({ k: 'lib-ready' });
    expect(readBridgeMessage({ type: 'tk:tweaks-panel', open: true })).toEqual({
      k: 'tweaks-panel',
      open: true,
    });
    expect(readBridgeMessage({ type: 'tk:tweaks-panel', open: 'yes' })).toBeNull();
    expect(readBridgeMessage({ type: 'tk:tweaks', controls: 'x' })).toBeNull();
  });
});
