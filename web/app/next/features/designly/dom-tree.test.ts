import { describe, expect, it } from 'vitest';
import {
  domRows,
  markOf,
  nodeAt,
  pathKey,
  pathsTo,
  selectorOf,
  withNode,
  type IDomNode,
} from '@/next/features/designly/dom-tree';

const node = (tag: string, over: Partial<IDomNode> = {}): IDomNode => ({
  tag,
  id: '',
  cls: '',
  text: '',
  n: 0,
  kids: null,
  more: 0,
  ...over,
});

// html > (head, body > (header, main > section))
const section = node('section');
const main = node('main', { n: 1, kids: [section] });
const header = node('header', { cls: 'nav top', id: 'nav' });
const body = node('body', { n: 2, kids: [header, main] });
const head = node('head', { n: 3 });
const html = node('html', { n: 2, kids: [head, body] });

describe('nodeAt', () => {
  it('finds a node by the indices of its path, and nothing past what is told', () => {
    expect(nodeAt(html, [])?.tag).toBe('html');
    expect(nodeAt(html, [1, 1, 0])?.tag).toBe('section');
    expect(nodeAt(html, [0, 0])).toBeNull();
    expect(nodeAt(html, [5])).toBeNull();
    expect(nodeAt(null, [])).toBeNull();
  });
});

describe('withNode', () => {
  it('puts a branch the page told into its place and leaves the rest as it was', () => {
    const told = node('head', { n: 1, kids: [node('title')] });
    const next = withNode(html, [0], told);
    expect(nodeAt(next, [0, 0])?.tag).toBe('title');
    expect(next?.kids?.[1]).toBe(body);
    expect(nodeAt(html, [0, 0])).toBeNull();
  });

  it('a path whose way is not told yet changes nothing', () => {
    expect(withNode(html, [0, 1], node('x'))).toBe(html);
    expect(withNode(html, [9], node('x'))).toBe(html);
  });

  it('the root path replaces the tree', () => {
    expect(withNode(html, [], body)).toBe(body);
    expect(withNode(null, [], body)).toBe(body);
  });
});

describe('domRows', () => {
  it('lists the opened part, with the depth, and a node with children not told yet is still expandable', () => {
    const rows = domRows(html, new Set(['', '1']));
    expect(rows.map((r) => `${r.depth}:${r.node.tag}`)).toEqual([
      '0:html',
      '1:head',
      '1:body',
      '2:header',
      '2:main',
    ]);
    expect(rows.find((r) => r.node.tag === 'head')?.expandable).toBe(true);
    expect(rows.find((r) => r.node.tag === 'main')?.open).toBe(false);
    expect(rows.find((r) => r.node.tag === 'header')?.expandable).toBe(false);
  });

  it('a closed root shows only itself, and a key for a node without children opens nothing', () => {
    expect(domRows(html, new Set()).map((r) => r.node.tag)).toEqual(['html']);
    expect(domRows(html, new Set(['', '1', '1.0'])).find((r) => r.node.tag === 'header')?.open).toBe(false);
    expect(domRows(null, new Set())).toEqual([]);
  });
});

describe('names', () => {
  it('reads a node as a selector reads it', () => {
    expect(selectorOf(header)).toBe('header#nav.nav.top');
    expect(selectorOf(main)).toBe('main');
    expect(markOf(header)).toBe('#nav.nav.top');
    expect(markOf(main)).toBe('');
  });

  it('keys a path and lists the paths down to it', () => {
    expect(pathKey([])).toBe('');
    expect(pathKey([1, 0])).toBe('1.0');
    expect(pathsTo([1, 0])).toEqual([[], [1], [1, 0]]);
  });
});
