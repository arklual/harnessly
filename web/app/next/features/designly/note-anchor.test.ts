import { describe, expect, it, vi } from 'vitest';
import {
  anchorItems,
  createAnchorStore,
  DRAFT_ANCHOR,
  fixedPin,
  PIN_PLACE,
  placePin,
} from '@/next/features/designly/note-anchor';
import { readBridgeMessage } from '@/next/features/designly/page-bridge';

const box = { x: 20, y: 100, w: 300, h: 80 };

describe('placePin', () => {
  it('stands on the whole block when all of it is on screen', () => {
    expect(placePin({ box, shown: box, covered: false }, 400, 800)).toEqual({
      place: PIN_PLACE.shown,
      box,
      cut: { t: -6, r: -6, b: -6, l: -6 },
      mark: { x: 0, y: 0 },
    });
  });

  it('cuts the frame to the visible part and puts the number at its corner', () => {
    // A list cut the block's top 30 px, a sidebar its right 100 px.
    const shown = { x: 20, y: 130, w: 200, h: 50 };
    expect(placePin({ box, shown, covered: false }, 400, 800)).toEqual({
      place: PIN_PLACE.shown,
      box,
      cut: { t: 30, r: 100, b: -6, l: -6 },
      mark: { x: 0, y: 30 },
    });
  });

  it('keeps the number whole inside the viewport', () => {
    const top = { x: 0, y: -40, w: 300, h: 80 };
    const at = placePin({ box: top, shown: { x: 0, y: 0, w: 300, h: 40 }, covered: false }, 400, 800);
    expect(at.mark).toEqual({ x: 14, y: 54 });
    expect(at.cut.t).toBe(40);
  });

  it('hides when the block is gone, out of sight or covered', () => {
    expect(placePin({ box: null, shown: null, covered: false }, 400, 800).place).toBe(PIN_PLACE.hidden);
    expect(placePin({ box, shown: null, covered: false }, 400, 800).place).toBe(PIN_PLACE.hidden);
    expect(placePin({ box, shown: null, covered: true }, 400, 800).place).toBe(PIN_PLACE.hidden);
  });
});

describe('fixedPin', () => {
  it('stands on the box of the pick for a page that does not follow its blocks', () => {
    expect(fixedPin({ x: 1, y: 2, width: 3, height: 4 })).toEqual({
      place: PIN_PLACE.shown,
      box: { x: 1, y: 2, w: 3, h: 4 },
      cut: { t: -6, r: -6, b: -6, l: -6 },
      mark: { x: 0, y: 0 },
    });
  });
});

describe('anchorItems', () => {
  const picked = (id: number | null) => ({ id, selector: `#b${id}` });
  it('follows the notes and the block of a new card', () => {
    const notes = [{ id: 'n1', picked: picked(1) }];
    expect(anchorItems(notes, null)).toEqual([{ key: 'n1', pick: 1, selector: '#b1' }]);
    expect(anchorItems(notes, { picked: picked(null) })).toEqual([
      { key: 'n1', pick: 1, selector: '#b1' },
      { key: DRAFT_ANCHOR, pick: null, selector: '#bnull' },
    ]);
    // A note being changed is already followed.
    expect(anchorItems(notes, { id: 'n1', picked: picked(1) })).toHaveLength(1);
  });
});

describe('the store and the page word', () => {
  it('hands the last word to whoever listens', () => {
    const store = createAnchorStore();
    const listener = vi.fn();
    const stop = store.subscribe(listener);
    const message = readBridgeMessage({
      type: 'tk:anchored',
      vw: 390,
      vh: 844,
      items: [
        { key: 'n1', box: [0, 10, 100, 50], shown: [0, 20, 100, 40], covered: false },
        { key: 'n2', box: null, shown: null, covered: false },
        { key: 'n3', box: [0, 0, 'x', 1], shown: [0, 0, -1, 1], covered: 'yes' },
      ],
    });
    expect(message?.k).toBe('anchored');
    if (message?.k !== 'anchored') return;
    store.set(message.report);
    expect(listener).toHaveBeenCalledTimes(1);
    expect(store.get()?.items.get('n1')).toEqual({
      box: { x: 0, y: 10, w: 100, h: 50 },
      shown: { x: 0, y: 20, w: 100, h: 40 },
      covered: false,
    });
    expect(store.get()?.items.get('n2')).toEqual({ box: null, shown: null, covered: false });
    // What is not right is no box.
    expect(store.get()?.items.get('n3')).toEqual({ box: null, shown: null, covered: false });
    stop();
    store.set(null);
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('drops a word without its viewport', () => {
    expect(readBridgeMessage({ type: 'tk:anchored', items: [] })).toBeNull();
  });
});
