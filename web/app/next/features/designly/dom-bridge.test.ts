import { describe, expect, it } from 'vitest';
import { readDomMessage } from '@/next/features/designly/dom-bridge';

const leaf = { tag: 'h1', n: 0, id: 'hero', cls: 'big', text: 'Куда сходить' };

describe('readDomMessage', () => {
  it('reads a branch of the tree with only the fields the panel draws', () => {
    const message = readDomMessage({
      type: 'tk:dom-tree',
      req: 3,
      path: [1],
      node: { tag: 'body', n: 1, kids: [leaf], onclick: 'steal()', attrs: { href: 'x' } },
    });
    expect(message).toEqual({
      k: 'tree',
      req: 3,
      path: [1],
      node: {
        tag: 'body',
        id: '',
        cls: '',
        text: '',
        n: 1,
        more: 0,
        kids: [{ tag: 'h1', id: 'hero', cls: 'big', text: 'Куда сходить', n: 0, more: 0, kids: null }],
      },
    });
  });

  it('clips what is long and drops what is not a node', () => {
    const message = readDomMessage({
      type: 'tk:dom-tree',
      req: 1,
      path: [],
      node: {
        tag: 'x'.repeat(100),
        n: 2,
        text: 'w'.repeat(500),
        cls: 'c'.repeat(500),
        kids: [leaf, 7, { n: 1 }],
      },
    });
    if (message?.k !== 'tree' || !message.node) throw new Error('no tree');
    expect(message.node.tag).toHaveLength(32);
    expect(message.node.text).toHaveLength(60);
    expect(message.node.cls).toHaveLength(120);
    expect(message.node.kids).toHaveLength(1);
  });

  it('stops at a depth and at a count a page could use to flood the panel', () => {
    let deep: Record<string, unknown> = { tag: 'i', n: 0 };
    for (let i = 0; i < 20; i += 1) deep = { tag: 'b', n: 1, kids: [deep] };
    const message = readDomMessage({ type: 'tk:dom-tree', req: 1, path: [], node: deep });
    let depth = 0;
    let at = message?.k === 'tree' ? message.node : null;
    while (at?.kids?.[0]) {
      at = at.kids[0];
      depth += 1;
    }
    expect(depth).toBeLessThanOrEqual(8);
    const wide = { tag: 'ul', n: 600, kids: Array.from({ length: 600 }, () => ({ tag: 'li', n: 0 })) };
    const flat = readDomMessage({ type: 'tk:dom-tree', req: 1, path: [], node: wide });
    expect(flat?.k === 'tree' && flat.node?.kids).toHaveLength(120);
  });

  it('a path that leads nowhere now is a tree without a node', () => {
    expect(readDomMessage({ type: 'tk:dom-tree', req: 2, path: [4], gone: true })).toEqual({
      k: 'tree',
      req: 2,
      path: [4],
      node: null,
    });
  });

  it('refuses a path that is not a list of indices, and an answer without its number', () => {
    expect(readDomMessage({ type: 'tk:dom-tree', req: 1, path: ['a'], node: leaf })).toBeNull();
    expect(readDomMessage({ type: 'tk:dom-tree', req: 1, path: [-1], node: leaf })).toBeNull();
    expect(
      readDomMessage({ type: 'tk:dom-tree', req: 1, path: new Array(65).fill(0), node: leaf }),
    ).toBeNull();
    expect(readDomMessage({ type: 'tk:dom-tree', path: [], node: leaf })).toBeNull();
    expect(readDomMessage({ type: 'tk:dom-picked', path: 'x' })).toBeNull();
  });

  it('reads a node: its box, its viewport and only the styles the panel lists', () => {
    const message = readDomMessage({
      type: 'tk:dom-node',
      req: 5,
      path: [1, 0],
      tag: 'header',
      id: 'nav',
      cls: 'top',
      text: 'Menu',
      rect: { x: 0, y: 12, w: 1440, h: 80 },
      viewport: { w: 1440, h: 900 },
      styles: { display: 'flex', color: 'rgb(0, 0, 0)', 'background-image': 'url(//evil)', margin: '' },
    });
    expect(message).toEqual({
      k: 'node',
      req: 5,
      path: [1, 0],
      detail: {
        path: [1, 0],
        tag: 'header',
        id: 'nav',
        cls: 'top',
        text: 'Menu',
        rect: { x: 0, y: 12, w: 1440, h: 80 },
        viewport: { w: 1440, h: 900 },
        styles: [
          { name: 'display', value: 'flex' },
          { name: 'color', value: 'rgb(0, 0, 0)' },
        ],
      },
    });
    expect(readDomMessage({ type: 'tk:dom-node', req: 5, path: [], rect: { x: 'a' } })).toMatchObject({
      detail: null,
    });
  });

  it('reads a pick, the end of inspecting, a change and the page being there', () => {
    expect(readDomMessage({ type: 'tk:dom-picked', path: [1, 2] })).toEqual({ k: 'picked', path: [1, 2] });
    expect(readDomMessage({ type: 'tk:dom-inspect', on: false })).toEqual({ k: 'inspect-off' });
    expect(readDomMessage({ type: 'tk:dom-inspect', on: true })).toBeNull();
    expect(readDomMessage({ type: 'tk:dom-changed' })).toEqual({ k: 'changed' });
    expect(readDomMessage({ type: 'tk:lib-ready' })).toEqual({ k: 'ready' });
    expect(readDomMessage({ type: 'other' })).toBeNull();
    expect(readDomMessage('tk:dom-changed')).toBeNull();
  });
});
