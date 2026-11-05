import { describe, expect, it } from 'vitest';
import { readLiveMessage, virtualKeyOf } from './live-browser';

describe('readLiveMessage', () => {
  it('reads an action’s outcome with its element’s box', () => {
    expect(
      readLiveMessage({
        t: 'acted',
        id: 'a1',
        ok: true,
        text: 'clicked link "x"',
        rect: { x: 1, y: 2, width: 3, height: 4 },
      }),
    ).toEqual({
      t: 'acted',
      id: 'a1',
      ok: true,
      text: 'clicked link "x"',
      rect: { x: 1, y: 2, width: 3, height: 4 },
    });
  });
  it('drops an outcome without an id and a box that is not one', () => {
    expect(readLiveMessage({ t: 'acted', ok: true, text: 'x' })).toBeNull();
    expect(readLiveMessage({ t: 'acted', id: 'a', ok: 'yes', text: 7, rect: { x: 'a' } })).toEqual({
      t: 'acted',
      id: 'a',
      ok: false,
      text: '',
      rect: null,
    });
  });
  it('reads the page and refuses the unknown', () => {
    expect(
      readLiveMessage({ t: 'page', url: 'https://a.test/', title: 'A', canBack: true, loading: false }),
    ).toEqual({
      t: 'page',
      url: 'https://a.test/',
      title: 'A',
      canBack: true,
      canForward: false,
      loading: false,
    });
    expect(readLiveMessage({ t: 'dance' })).toBeNull();
    expect(readLiveMessage('page')).toBeNull();
  });
});

describe('virtualKeyOf', () => {
  it('knows letters, digits and the named keys', () => {
    expect(virtualKeyOf('a', 'KeyA')).toBe(65);
    expect(virtualKeyOf('5', 'Digit5')).toBe(53);
    expect(virtualKeyOf('Enter', 'Enter')).toBe(13);
    expect(virtualKeyOf('ArrowDown', 'ArrowDown')).toBe(40);
    expect(virtualKeyOf('Dead', 'Quote')).toBe(0);
  });
});
