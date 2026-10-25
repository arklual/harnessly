// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DOM_STATUS, useDomBridge } from '@/next/features/designly/use-dom-bridge';

const SRC = 'https://slot-1.preview.example/enter?t=1';
const ORIGIN = 'https://slot-1.preview.example';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const leaf = (tag: string) => ({ tag, n: 0 });
const tree = {
  tag: 'html',
  n: 2,
  kids: [
    { tag: 'head', n: 1 },
    { tag: 'body', n: 2, kids: [leaf('header'), { tag: 'main', n: 1 }] },
  ],
};

/** A frame whose window records what the bridge posts to it. */
const setup = (active = true, blank = false) => {
  const posted: { message: Record<string, unknown>; target: string }[] = [];
  const win = {
    ...(blank ? { location: { href: 'about:blank' } } : {}),
    postMessage: (message: Record<string, unknown>, target: string) => posted.push({ message, target }),
  };
  const frameRef = { current: { contentWindow: win } as unknown as HTMLIFrameElement };
  const view = renderHook((props: { active: boolean }) => useDomBridge(frameRef, SRC, 0, props.active), {
    initialProps: { active },
  });
  /** The page says something: from its window and its origin, unless told otherwise. */
  const say = (data: unknown, from: { source?: unknown; origin?: string } = {}) =>
    act(() => {
      const event = new MessageEvent('message', { data, origin: from.origin ?? ORIGIN });
      Object.defineProperty(event, 'source', { value: from.source ?? win });
      window.dispatchEvent(event);
    });
  const last = (type: string) => [...posted].reverse().find((p) => p.message.type === type);
  return { view, posted, say, last, win };
};

const answerTree = async (s: ReturnType<typeof setup>, node: unknown, path: number[] = []) => {
  const ask = s.last('tk:dom-tree');
  if (!ask) throw new Error('nothing asked');
  await s.say({ type: 'tk:dom-tree', req: ask.message.req, path, node });
};

describe('useDomBridge', () => {
  it("asks the page for its tree only while the tab is in view, and only at the frame's origin", async () => {
    const s = setup(false);
    expect(s.last('tk:dom-tree')).toBeUndefined();
    s.view.rerender({ active: true });
    const ask = s.last('tk:dom-tree');
    expect(ask?.target).toBe(ORIGIN);
    expect(ask?.message).toMatchObject({ path: [], depth: 3 });
    expect(s.last('tk:dom-watch')?.message.on).toBe(true);
    expect(s.view.result.current.status).toBe(DOM_STATUS.loading);
    await answerTree(s, tree);
    expect(s.view.result.current.status).toBe(DOM_STATUS.ready);
    // The document and its body are open, as in any browser's tools.
    expect(s.view.result.current.rows.map((r) => r.node.tag)).toEqual([
      'html',
      'head',
      'body',
      'header',
      'main',
    ]);
    s.view.rerender({ active: false });
    expect(s.last('tk:dom-mark')?.message.path).toBeNull();
    expect(s.last('tk:dom-watch')?.message.on).toBe(false);
  });

  it('a page that does not answer is silent, and can be asked again', async () => {
    const s = setup();
    await act(async () => void vi.advanceTimersByTime(2600));
    expect(s.view.result.current.status).toBe(DOM_STATUS.silent);
    act(() => s.view.result.current.reload());
    expect(s.view.result.current.status).toBe(DOM_STATUS.loading);
    await answerTree(s, tree);
    expect(s.view.result.current.status).toBe(DOM_STATUS.ready);
  });

  it('does not take an answer from another window or another origin', async () => {
    const s = setup();
    const ask = s.last('tk:dom-tree');
    const reply = { type: 'tk:dom-tree', req: ask?.message.req, path: [], node: tree };
    await s.say(reply, { origin: 'https://evil.example' });
    await s.say(reply, { source: {} });
    expect(s.view.result.current.status).toBe(DOM_STATUS.loading);
    await s.say(reply);
    expect(s.view.result.current.status).toBe(DOM_STATUS.ready);
  });

  it('opens a node that was never asked for by asking for it first', async () => {
    const s = setup();
    await answerTree(s, tree);
    const main = s.view.result.current.rows.find((r) => r.node.tag === 'main');
    if (!main) throw new Error('no main');
    act(() => s.view.result.current.toggle(main.path));
    expect(s.last('tk:dom-tree')?.message).toMatchObject({ path: [1, 1], depth: 2 });
    await answerTree(s, { tag: 'main', n: 1, kids: [leaf('section')] }, [1, 1]);
    expect(s.view.result.current.rows.map((r) => r.node.tag)).toContain('section');
    act(() => s.view.result.current.toggle(main.path));
    expect(s.view.result.current.rows.map((r) => r.node.tag)).not.toContain('section');
  });

  it('lights a node on the page for its row, and takes it with its box and styles', async () => {
    const s = setup();
    await answerTree(s, tree);
    act(() => s.view.result.current.hover([1, 0]));
    expect(s.last('tk:dom-mark')?.message).toMatchObject({ path: [1, 0], reveal: false });
    act(() => s.view.result.current.select([1, 0]));
    expect(s.last('tk:dom-mark')?.message).toMatchObject({ path: [1, 0], reveal: true });
    const ask = s.last('tk:dom-node');
    await s.say({
      type: 'tk:dom-node',
      req: ask?.message.req,
      path: [1, 0],
      tag: 'header',
      rect: { x: 0, y: 0, w: 100, h: 50 },
      viewport: { w: 1440, h: 900 },
      styles: { display: 'flex' },
    });
    expect(s.view.result.current.detail?.rect.w).toBe(100);
    // After the pointer leaves the tree the light goes back to the taken node.
    act(() => s.view.result.current.hover(null));
    expect(s.last('tk:dom-mark')?.message.path).toEqual([1, 0]);
  });

  it('inspecting: a click in the page picks a node, the way down to it is opened and the node is taken', async () => {
    const s = setup();
    await answerTree(s, tree);
    act(() => s.view.result.current.setInspect(true));
    expect(s.last('tk:dom-inspect')?.message.on).toBe(true);
    expect(s.view.result.current.inspect).toBe(true);
    await s.say({ type: 'tk:dom-picked', path: [1, 1, 0] });
    // The picked node is under «main», whose children are not told yet: they are asked for.
    expect(s.last('tk:dom-tree')?.message).toMatchObject({ path: [1, 1], depth: 1 });
    await answerTree(s, { tag: 'main', n: 1, kids: [leaf('section')] }, [1, 1]);
    expect(s.view.result.current.selected).toEqual([1, 1, 0]);
    expect(s.view.result.current.inspect).toBe(false);
    expect(s.last('tk:dom-node')?.message.path).toEqual([1, 1, 0]);
  });

  it('the page ending the inspecting itself, and the tab going out of view, turn it off', async () => {
    const s = setup();
    await answerTree(s, tree);
    act(() => s.view.result.current.setInspect(true));
    await s.say({ type: 'tk:dom-inspect', on: false });
    expect(s.view.result.current.inspect).toBe(false);
    act(() => s.view.result.current.setInspect(true));
    s.view.rerender({ active: false });
    expect(s.last('tk:dom-inspect')?.message.on).toBe(false);
  });

  it('a change of the page reads the tree again once, after a burst', async () => {
    const s = setup();
    await answerTree(s, tree);
    const before = s.posted.filter((p) => p.message.type === 'tk:dom-tree').length;
    await s.say({ type: 'tk:dom-changed' });
    await s.say({ type: 'tk:dom-changed' });
    await act(async () => void vi.advanceTimersByTime(400));
    expect(s.posted.filter((p) => p.message.type === 'tk:dom-tree').length).toBe(before + 1);
  });

  it('says nothing to a frame whose page has not come yet: it is still a blank page of this window', () => {
    const s = setup(false, true);
    s.view.rerender({ active: true });
    expect(s.posted).toEqual([]);
  });
});
