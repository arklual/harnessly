import { describe, expect, it } from 'vitest';
import * as core from './bridge-core';

interface IFake {
  nodeType: 1;
  parentElement: IFake | null;
  style: Record<string, string>;
  rect: { left: number; top: number; width: number; height: number };
  offsetWidth: number;
  offsetHeight: number;
  clientLeft: number;
  clientTop: number;
  clientWidth: number;
  clientHeight: number;
  getBoundingClientRect: () => IFake['rect'];
}

/** An element with its box (in the viewport), its computed style and, when it scrolls, its padding box. */
const node = (
  parent: IFake | null,
  rect: IFake['rect'],
  style: Record<string, string> = {},
  scale = 1,
): IFake => {
  const el: IFake = {
    nodeType: 1,
    parentElement: parent,
    style: {
      position: 'static',
      overflowX: 'visible',
      overflowY: 'visible',
      pointerEvents: 'auto',
      ...style,
    },
    rect,
    offsetWidth: rect.width / scale,
    offsetHeight: rect.height / scale,
    clientLeft: 0,
    clientTop: 0,
    clientWidth: rect.width / scale,
    clientHeight: rect.height / scale,
    getBoundingClientRect: () => el.rect,
  };
  return el;
};

const pageOf = (width = 400, height = 800) => {
  const html = node(null, { left: 0, top: 0, width, height });
  const body = node(html, { left: 0, top: 0, width, height: 3000 });
  const win = { innerWidth: width, innerHeight: height, getComputedStyle: (el: IFake) => el.style };
  const doc: Record<string, unknown> = { documentElement: html, body, elementFromPoint: () => null };
  return { html, body, win, doc };
};

/** Hit-testing as the page answers it: the last of `layers` whose box holds the point, else `under`. */
const hitTest =
  (under: IFake, layers: IFake[]) =>
  (x: number, y: number): IFake => {
    for (const layer of [...layers].reverse()) {
      const r = layer.rect;
      if (x >= r.left && x < r.left + r.width && y >= r.top && y < r.top + r.height) return layer;
    }
    return under;
  };

describe('meetBoxes and trimBox', () => {
  it('meets two boxes, or none', () => {
    expect(core.meetBoxes({ x: 0, y: 0, w: 100, h: 100 }, { x: 50, y: 60, w: 100, h: 100 })).toEqual({
      x: 50,
      y: 60,
      w: 50,
      h: 40,
    });
    expect(core.meetBoxes({ x: 0, y: 0, w: 10, h: 10 }, { x: 10, y: 0, w: 10, h: 10 })).toBeNull();
  });

  it('cuts off a side that something lies over whole, and nothing else', () => {
    const card = { x: 0, y: 100, w: 300, h: 100 };
    // A sticky header over the card's top.
    expect(core.trimBox(card, { x: 0, y: 0, w: 400, h: 140 })).toEqual({ x: 0, y: 140, w: 300, h: 60 });
    // A sheet from the bottom.
    expect(core.trimBox(card, { x: 0, y: 180, w: 400, h: 600 })).toEqual({ x: 0, y: 100, w: 300, h: 80 });
    // A sidebar from the right.
    expect(core.trimBox(card, { x: 200, y: 0, w: 200, h: 800 })).toEqual({ x: 0, y: 100, w: 200, h: 100 });
    // A badge in the middle cuts nothing.
    expect(core.trimBox(card, { x: 100, y: 120, w: 20, h: 20 })).toEqual(card);
    // All of it covered.
    expect(core.trimBox(card, { x: 0, y: 0, w: 400, h: 800 })).toBeNull();
  });
});

describe('clipByAncestors', () => {
  it('cuts a block to every scroller it sits in, each axis on its own', () => {
    const { body, win, doc } = pageOf();
    const list = node(
      body,
      { left: 20, top: 100, width: 360, height: 200 },
      { overflowX: 'hidden', overflowY: 'auto' },
    );
    const row = node(list, { left: 0, top: 0, width: 2000, height: 3000 });
    const chips = node(
      row,
      { left: 40, top: 150, width: 400, height: 40 },
      { overflowX: 'auto', overflowY: 'hidden' },
    );
    const chip = node(chips, { left: 300, top: 140, width: 120, height: 60 });
    const box = { x: 300, y: 140, w: 120, h: 60 };
    expect(core.clipByAncestors(chip, box, win, doc, core)).toEqual({ x: 300, y: 150, w: 80, h: 40 });
    // Scrolled out of the list: nothing is left.
    list.rect = { left: 20, top: 300, width: 360, height: 200 };
    expect(core.clipByAncestors(chip, box, win, doc, core)).toBeNull();
  });

  it('takes a scaled scroller at its size on screen', () => {
    const { body, win, doc } = pageOf();
    // Drawn at half its size: 200×100 of its own pixels are 100×50 on screen.
    const panel = node(
      body,
      { left: 10, top: 10, width: 100, height: 50 },
      { overflowX: 'hidden', overflowY: 'hidden' },
      0.5,
    );
    panel.clientLeft = 4;
    panel.clientTop = 4;
    panel.clientWidth = 192;
    panel.clientHeight = 92;
    const item = node(panel, { left: 0, top: 0, width: 400, height: 400 });
    expect(core.clipByAncestors(item, { x: 0, y: 0, w: 400, h: 400 }, win, doc, core)).toEqual({
      x: 12,
      y: 12,
      w: 96,
      h: 46,
    });
  });

  it('lets a fixed box escape its scrollers, an absolute one those under its positioned ancestor', () => {
    const { body, win, doc } = pageOf();
    const scroller = node(
      body,
      { left: 0, top: 0, width: 100, height: 100 },
      { overflowX: 'auto', overflowY: 'auto' },
    );
    const box = { x: 0, y: 200, w: 50, h: 50 };
    const fixed = node(scroller, { left: 0, top: 200, width: 50, height: 50 }, { position: 'fixed' });
    expect(core.clipByAncestors(fixed, box, win, doc, core)).toEqual(box);
    // A transform holds fixed boxes: its scroller clips again.
    scroller.style.transform = 'translateX(0px)';
    expect(core.clipByAncestors(fixed, box, win, doc, core)).toBeNull();

    const positioned = node(body, { left: 0, top: 0, width: 400, height: 400 }, { position: 'relative' });
    const clipper = node(
      positioned,
      { left: 0, top: 0, width: 100, height: 100 },
      { overflowX: 'hidden', overflowY: 'hidden' },
    );
    const absolute = node(clipper, { left: 0, top: 200, width: 50, height: 50 }, { position: 'absolute' });
    expect(core.clipByAncestors(absolute, box, win, doc, core)).toEqual(box);
  });
});

describe('uncover', () => {
  it('cuts off what a sidebar lies over and hides what it covers whole', () => {
    const { body, win, doc } = pageOf();
    const card = node(body, { left: 0, top: 100, width: 300, height: 100 });
    const side = node(body, { left: 200, top: 0, width: 200, height: 800 }, { position: 'fixed' });
    const link = node(side, { left: 220, top: 120, width: 40, height: 20 });
    doc.elementFromPoint = (x: number) => (x >= 200 ? link : card);
    const shown = { x: 0, y: 100, w: 300, h: 100 };
    // The link answers, the sidebar it is in is what lies over the card.
    expect(core.uncover(card, shown, win, doc, core)).toEqual({
      shown: { x: 0, y: 100, w: 200, h: 100 },
      covered: false,
    });
    side.rect = { left: 0, top: 0, width: 400, height: 800 };
    doc.elementFromPoint = () => link;
    expect(core.uncover(card, shown, win, doc, core)).toEqual({ shown: null, covered: true });
  });

  it('takes the block, what is inside it and its ancestors as its own', () => {
    const { body, win, doc } = pageOf();
    const card = node(body, { left: 0, top: 100, width: 300, height: 100 });
    const title = node(card, { left: 10, top: 110, width: 100, height: 20 });
    doc.elementFromPoint = hitTest(body, [card, title]);
    const shown = { x: 0, y: 100, w: 300, h: 100 };
    expect(core.uncover(card, shown, win, doc, core)).toEqual({ shown, covered: false });
  });

  it('keeps a block that is only partly under something it cannot cut off', () => {
    const { body, win, doc } = pageOf();
    const card = node(body, { left: 0, top: 100, width: 300, height: 100 });
    const badge = node(body, { left: 280, top: 90, width: 30, height: 30 }, { position: 'absolute' });
    doc.elementFromPoint = hitTest(body, [card, badge]);
    const shown = { x: 0, y: 100, w: 300, h: 100 };
    expect(core.uncover(card, shown, win, doc, core)).toEqual({ shown, covered: false });
  });

  it('does not test a block the pointer goes through', () => {
    const { body, win, doc } = pageOf();
    const label = node(body, { left: 0, top: 0, width: 50, height: 50 }, { pointerEvents: 'none' });
    const picture = node(body, { left: 0, top: 0, width: 400, height: 400 });
    doc.elementFromPoint = () => picture;
    const shown = { x: 0, y: 0, w: 50, h: 50 };
    expect(core.uncover(label, shown, win, doc, core)).toEqual({ shown, covered: false });
  });
});

describe('anchorGeometry', () => {
  it('gives the box, and what of it the scrollers, the viewport and the page leave', () => {
    const { body, win, doc } = pageOf(400, 300);
    const card = node(body, { left: 0, top: 250, width: 300, height: 100 });
    doc.elementFromPoint = hitTest(body, [card]);
    expect(core.anchorGeometry(card, win, doc, core)).toEqual({
      box: { x: 0, y: 250, w: 300, h: 100 },
      shown: { x: 0, y: 250, w: 300, h: 50 },
      covered: false,
    });
    card.rect = { left: 0, top: 400, width: 300, height: 100 };
    expect(core.anchorGeometry(card, win, doc, core)).toEqual({
      box: { x: 0, y: 400, w: 300, h: 100 },
      shown: null,
      covered: false,
    });
  });
});

describe('parseAnchorItems', () => {
  it('takes keyed blocks with a number or a selector, and drops a set with one wrong item', () => {
    const items = [
      { key: 'n1', pick: 3, selector: 'div.card' },
      { key: '~draft', pick: null, selector: '' },
    ];
    expect(core.parseAnchorItems(items)).toEqual(items);
    expect(core.parseParentMessage({ type: 'tk:anchors', items })).toEqual({ type: 'tk:anchors', items });
    for (const bad of [
      { key: '', pick: 1, selector: 'a' },
      { key: 'a b', pick: 1, selector: 'a' },
      { key: 'n', pick: 0, selector: 'a' },
      { key: 'n', pick: 1.5, selector: 'a' },
      { key: 'n', pick: 1, selector: 'x'.repeat(513) },
      { key: 'n', pick: 1 },
      null,
    ])
      expect(core.parseAnchorItems([items[0], bad])).toBeNull();
    expect(
      core.parseAnchorItems(
        Array.from({ length: 65 }, (_, i) => ({ key: `n${i}`, pick: null, selector: '' })),
      ),
    ).toBeNull();
  });
});
