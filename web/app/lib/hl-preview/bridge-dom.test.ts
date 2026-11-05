// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as core from './bridge-core';

const BOX = { left: 10, top: 40, width: 100, height: 30, right: 110, bottom: 70, x: 10, y: 40 };

interface ITree {
  tag: string;
  n: number;
  kids: ITree[];
  more?: number;
}
/** Whatever the bridge posts; a test reads only the fields of the message it asked for. */
interface ISent {
  type: string;
  node: ITree;
  styles: Record<string, string>;
  [key: string]: unknown;
}
type TSent = Record<string, unknown>;

describe('tk:dom-* parsing', () => {
  const parse = (fields: Record<string, unknown>) =>
    core.parseParentMessage({ type: 'tk:dom-tree', ...fields });

  it('takes well-formed messages and clamps the depth', () => {
    expect(core.parseParentMessage({ type: 'tk:dom-on' })).toEqual({ type: 'tk:dom-on' });
    expect(parse({ req: 3, path: [0, 2], depth: 2 })).toEqual({
      type: 'tk:dom-tree',
      req: 3,
      path: [0, 2],
      depth: 2,
    });
    expect(parse({ req: 3, path: [], depth: 99 })).toMatchObject({ depth: 6 });
    expect(parse({ req: 3, path: [], depth: -4 })).toMatchObject({ depth: 0 });
    expect(parse({ req: 3, path: [] })).toMatchObject({ depth: 1 });
    expect(core.parseParentMessage({ type: 'tk:dom-node', req: 1, path: [1] })).toEqual({
      type: 'tk:dom-node',
      req: 1,
      path: [1],
    });
    expect(core.parseParentMessage({ type: 'tk:dom-mark', path: null, reveal: false })).toEqual({
      type: 'tk:dom-mark',
      path: null,
      reveal: false,
    });
    expect(core.parseParentMessage({ type: 'tk:dom-mark', path: [0] })).toEqual({
      type: 'tk:dom-mark',
      path: [0],
      reveal: false,
    });
    expect(core.parseParentMessage({ type: 'tk:dom-inspect', on: true })).toEqual({
      type: 'tk:dom-inspect',
      on: true,
    });
    expect(core.parseParentMessage({ type: 'tk:dom-watch', on: false })).toEqual({
      type: 'tk:dom-watch',
      on: false,
    });
  });

  it('drops what is not right', () => {
    const bad: Record<string, unknown>[] = [
      { req: 1, path: [-1] },
      { req: 1, path: [1.5] },
      { req: 1, path: ['1'] },
      { req: 1, path: new Array(65).fill(0) },
      { req: 1, path: 'body' },
      { req: 1, path: [100001] },
      { path: [] },
      { req: '1', path: [] },
      { req: Number.NaN, path: [] },
      { req: 1, path: [], depth: '2' },
      { req: 1, path: [], depth: Number.POSITIVE_INFINITY },
    ];
    for (const fields of bad) {
      expect(parse(fields), JSON.stringify(fields)).toBeNull();
      if (!('depth' in fields))
        expect(
          core.parseParentMessage({ type: 'tk:dom-node', ...fields }),
          JSON.stringify(fields),
        ).toBeNull();
    }
    expect(core.parseParentMessage({ type: 'tk:dom-mark', path: undefined })).toBeNull();
    expect(core.parseParentMessage({ type: 'tk:dom-mark', path: [0], reveal: 'yes' })).toBeNull();
    expect(core.parseParentMessage({ type: 'tk:dom-inspect', on: 'yes' })).toBeNull();
    expect(core.parseParentMessage({ type: 'tk:dom-watch' })).toBeNull();
    expect(core.parseParentMessage({ type: 'tk:dom-other' })).toBeNull();
  });
});

describe('the DOM tools of the preview bridge', () => {
  let bound: Record<string, unknown>;
  let refs: core.IActRefs;
  let state: core.IDomState;
  let sent: ISent[];

  const post = (message: TSent) => {
    sent.push(message as ISent);
  };
  const send = (data: Record<string, unknown>) => {
    const message = core.parseParentMessage(data);
    if (!message || !core.isDomMessage(message)) return;
    core.handleDomMessage(window, document, bound, core, state, message, post);
  };
  const of = (type: string) => sent.filter((m) => m.type === type);
  const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

  beforeEach(() => {
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(() => BOX as DOMRect);
    document.head.innerHTML = '<title>t</title>';
    document.body.innerHTML = `
      <header id="top" class="bar  wide">Hello <b>x</b> world</header>
      <main><p>One</p><p style="color: red">Two</p></main>
      <script>1</script>`;
    bound = { ...core.captureNatives(window), origin: window.location.origin, parent: window };
    refs = core.makeActRefs();
    state = core.makeDomState(refs);
    sent = [];
  });
  afterEach(() => {
    send({ type: 'tk:dom-inspect', on: false });
    send({ type: 'tk:dom-watch', on: false });
    document.querySelectorAll('[data-tk-library-mark]').forEach((el) => el.remove());
    vi.restoreAllMocks();
  });

  it('stays silent until tk:dom-on, then says it is ready', () => {
    send({ type: 'tk:dom-tree', req: 1, path: [], depth: 1 });
    send({ type: 'tk:dom-node', req: 2, path: [] });
    send({ type: 'tk:dom-mark', path: [1], reveal: false });
    send({ type: 'tk:dom-inspect', on: true });
    send({ type: 'tk:dom-watch', on: true });
    expect(sent).toEqual([]);
    expect(document.querySelector('[data-tk-library-mark]')).toBeNull();
    send({ type: 'tk:dom-on' });
    expect(sent).toEqual([{ type: 'tk:lib-ready' }]);
  });

  it('answers a tree with depth and a node budget, with own text and no attribute values', () => {
    send({ type: 'tk:dom-on' });
    send({ type: 'tk:dom-tree', req: 7, path: [], depth: 2 });
    const [tree] = of('tk:dom-tree');
    expect(tree).toMatchObject({ req: 7, path: [] });
    expect(tree.node).toMatchObject({ tag: 'html', n: 2 });
    const body = tree.node.kids[1];
    expect(body).toMatchObject({ tag: 'body', n: 3 });
    expect(body.kids[0]).toEqual({ tag: 'header', n: 1, id: 'top', cls: 'bar wide', text: 'Hello world' });
    // Depth 2 stops below the body's children.
    expect(body.kids[1].kids).toBeUndefined();
    expect(JSON.stringify(tree)).not.toContain('red');

    send({ type: 'tk:dom-tree', req: 8, path: [1, 1], depth: 1 });
    expect(of('tk:dom-tree')[1].node.kids.map((k) => k.tag)).toEqual(['p', 'p']);
  });

  it('cuts a wide tree to the node budget and counts what is left out', () => {
    document.body.innerHTML = `<div>${'<i></i>'.repeat(500)}</div>`;
    send({ type: 'tk:dom-on' });
    send({ type: 'tk:dom-tree', req: 1, path: [1, 0], depth: 6 });
    const node = of('tk:dom-tree')[0].node;
    expect(node.n).toBe(500);
    expect(node.kids).toHaveLength(120);
    expect(node.more).toBe(380);
  });

  it('says gone for a path that leads nowhere', () => {
    send({ type: 'tk:dom-on' });
    send({ type: 'tk:dom-tree', req: 4, path: [1, 9], depth: 1 });
    send({ type: 'tk:dom-node', req: 5, path: [7] });
    expect(of('tk:dom-tree')[0]).toEqual({ type: 'tk:dom-tree', req: 4, path: [1, 9], gone: true });
    expect(of('tk:dom-node')[0]).toEqual({ type: 'tk:dom-node', req: 5, path: [7], gone: true });
  });

  it('answers a node with its box and computed styles', () => {
    send({ type: 'tk:dom-on' });
    send({ type: 'tk:dom-node', req: 9, path: [1, 1, 1] });
    const [node] = of('tk:dom-node');
    expect(node).toMatchObject({
      req: 9,
      path: [1, 1, 1],
      tag: 'p',
      id: '',
      cls: '',
      text: 'Two',
      rect: { x: 10, y: 40, w: 100, h: 30 },
      viewport: { w: window.innerWidth, h: window.innerHeight },
    });
    expect(node.styles.color).toBe('rgb(255, 0, 0)');
    expect(Object.keys(node.styles)).toHaveLength(16);
    expect(typeof node.styles.margin).toBe('string');
  });

  it('places the mark over the node, takes it away, and keeps it out of paths and the agent snapshot', async () => {
    document.body.insertAdjacentHTML('beforeend', '<button>Buy</button>');
    send({ type: 'tk:dom-on' });
    send({ type: 'tk:dom-mark', path: [1, 0], reveal: false });
    const mark = document.querySelector<HTMLElement>('[data-tk-library-mark]')!;
    expect(mark.parentElement).toBe(document.documentElement);
    expect(mark.style.display).toBe('block');
    expect(mark.style.left).toBe('10px');
    expect(mark.style.width).toBe('100px');
    expect(mark.textContent).toContain('header#top.bar.wide');
    expect(mark.textContent).toContain('100 × 30');
    // The mark is the last child of <html> but is not counted in the tree.
    send({ type: 'tk:dom-tree', req: 1, path: [], depth: 0 });
    expect(of('tk:dom-tree')[0].node.n).toBe(2);
    expect(refs.skip).toBe(mark);
    const snapshot = await core.runAct(
      window,
      document,
      bound,
      core,
      refs,
      core.parseParentMessage({ type: 'tk:act', id: 'a', op: 'snapshot' }) as core.IActMessage,
    );
    expect(snapshot.text).toContain('"Buy"');
    expect(snapshot.text).not.toContain('header#top');
    send({ type: 'tk:dom-mark', path: null, reveal: false });
    expect(mark.style.display).toBe('none');
  });

  it('inspects: a click is swallowed and its node picked, then inspecting ends', () => {
    send({ type: 'tk:dom-on' });
    send({ type: 'tk:dom-inspect', on: true });
    const page = vi.fn();
    const target = document.querySelectorAll('main p')[1];
    document.body.addEventListener('click', page);
    target.dispatchEvent(new MouseEvent('mousemove', { bubbles: true }));
    expect(document.querySelector<HTMLElement>('[data-tk-library-mark]')!.style.display).toBe('block');
    const click = new MouseEvent('click', { bubbles: true, cancelable: true });
    target.dispatchEvent(click);
    expect(click.defaultPrevented).toBe(true);
    expect(page).not.toHaveBeenCalled();
    expect(of('tk:dom-picked')).toEqual([{ type: 'tk:dom-picked', path: [1, 1, 1] }]);
    expect(document.querySelector<HTMLElement>('[data-tk-library-mark]')!.style.display).toBe('none');
    // Done: the next click is the page's.
    target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    expect(page).toHaveBeenCalledTimes(1);
    expect(of('tk:dom-picked')).toHaveLength(1);
    document.body.removeEventListener('click', page);
  });

  it('ends inspecting on Escape and tells the parent', () => {
    send({ type: 'tk:dom-on' });
    send({ type: 'tk:dom-inspect', on: true });
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }));
    expect(of('tk:dom-inspect')).toEqual([]);
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(of('tk:dom-inspect')).toEqual([{ type: 'tk:dom-inspect', on: false }]);
    const page = vi.fn();
    document.body.addEventListener('click', page);
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(page).toHaveBeenCalledTimes(1);
    document.body.removeEventListener('click', page);
  });

  it('watches: a change of the page is reported once, the mark’s own changes are not', async () => {
    send({ type: 'tk:dom-on' });
    send({ type: 'tk:dom-watch', on: true });
    send({ type: 'tk:dom-mark', path: [1, 0], reveal: false });
    send({ type: 'tk:dom-mark', path: [1, 1, 0], reveal: false });
    await wait(550);
    expect(of('tk:dom-changed')).toEqual([]);
    document.querySelector('main')!.appendChild(document.createElement('p'));
    document.querySelector('main')!.appendChild(document.createElement('p'));
    await wait(550);
    expect(of('tk:dom-changed')).toHaveLength(1);
    document.getElementById('top')!.className = 'other';
    await wait(550);
    expect(of('tk:dom-changed')).toHaveLength(2);
    send({ type: 'tk:dom-watch', on: false });
    document.body.appendChild(document.createElement('div'));
    await wait(550);
    expect(of('tk:dom-changed')).toHaveLength(2);
  });

  it('drops bad messages without a word', () => {
    send({ type: 'tk:dom-on' });
    sent = [];
    send({ type: 'tk:dom-tree', req: 'x', path: [] });
    send({ type: 'tk:dom-node', req: 1, path: [-1] });
    send({ type: 'tk:dom-mark', path: 'body' });
    send({ type: 'tk:dom-inspect', on: 1 });
    expect(sent).toEqual([]);
  });

  it('never throws out of a handler when the parent or the page breaks', () => {
    send({ type: 'tk:dom-on' });
    const boom = () => {
      throw new Error('gone');
    };
    const message = core.parseParentMessage({
      type: 'tk:dom-tree',
      req: 1,
      path: [],
      depth: 1,
    }) as core.TDomMessageIn;
    expect(() => core.handleDomMessage(window, document, bound, core, state, message, boom)).not.toThrow();
    const broken = { ...bound, box: boom, style: boom };
    const node = core.parseParentMessage({ type: 'tk:dom-node', req: 1, path: [] }) as core.TDomMessageIn;
    expect(() => core.handleDomMessage(window, document, broken, core, state, node, post)).not.toThrow();
  });
});
