// @vitest-environment jsdom
import { act, cleanup, render } from '@testing-library/react';
import { Profiler, useRef, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import useDrag, {
  COMMIT_WAIT_MS,
  EDGE,
  SETTLE_MS,
  type IDragSource,
  type IDropTarget,
} from '@/next/ui/use-drag';

const ROW = 'row';
const KINDS: readonly (typeof ROW)[] = [ROW];
const HEIGHT = 40;

interface IPlan {
  id: string;
  to: number;
}

/** How many times the list has rendered (committed), counted by a Profiler around it. */
let renders = 0;
const counted = () => {
  renders += 1;
};

/** A list of rows down the page that keeps its own order: the drop reorders it at once, as a store would. */
const Rows = ({ onDrop }: { onDrop?: (plan: IPlan) => void }) => {
  const listRef = useRef<HTMLDivElement>(null);
  const [ids, setIds] = useState(['a', 'b', 'c', 'd']);
  const drag = useDrag({
    listRef,
    dragKinds: KINDS,
    dropKinds: KINDS,
    lined: () => true,
    resolve: (source: IDragSource<typeof ROW>, target: IDropTarget<typeof ROW>) => {
      const from = ids.indexOf(source.id);
      const over = ids.indexOf(target.id ?? '');
      const slot = over + (target.edge === EDGE.after ? 1 : 0);
      const to = from < slot ? slot - 1 : slot;
      return to === from ? null : { id: source.id, to };
    },
    onDrop: (plan: IPlan) => {
      onDrop?.(plan);
      setIds((now) => {
        const next = now.filter((id) => id !== plan.id);
        next.splice(plan.to, 0, plan.id);
        return next;
      });
    },
  });
  return (
    <div ref={listRef} data-testid="list" data-carrying={drag.dragging || undefined}>
      {ids.map((id) => (
        <div key={id} data-row={id} data-drag={ROW} data-drag-id={id} data-drop={ROW} data-drop-id={id}>
          {id}
        </div>
      ))}
    </div>
  );
};

const List = ({ onDrop }: { onDrop?: (plan: IPlan) => void }) => (
  <Profiler id="list" onRender={counted}>
    <Rows onDrop={onDrop} />
  </Profiler>
);

const row = (id: string) => document.querySelector<HTMLElement>(`[data-row="${id}"]`) as HTMLElement;
const list = () => document.querySelector<HTMLElement>('[data-testid="list"]') as HTMLElement;
const order = () => [...list().children].map((el) => (el as HTMLElement).dataset.row);

// jsdom lays nothing out: the rows stand in their DOM order, 40 px each, in a list of 160 from the top of the window.
const layOut = () => {
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const el = this as HTMLElement;
    const index = el.dataset.row ? [...(el.parentElement?.children ?? [])].indexOf(el) : 0;
    const top = index * HEIGHT;
    const height = el.dataset.row ? HEIGHT : HEIGHT * 4;
    return { top, bottom: top + height, height, left: 0, right: 200, width: 200, x: 0, y: top } as DOMRect;
  });
};

const pointer = (type: string, y: number) =>
  new PointerEvent(type, {
    bubbles: true,
    cancelable: true,
    clientX: 20,
    clientY: y,
    pointerId: 1,
    pointerType: 'mouse',
    button: 0,
    isPrimary: true,
  });
const press = (id: string, y: number) => act(() => void row(id).dispatchEvent(pointer('pointerdown', y)));
const moveTo = (y: number) =>
  act(() => {
    window.dispatchEvent(pointer('pointermove', y));
    vi.advanceTimersByTime(16);
  });
const release = (y: number) => act(() => void window.dispatchEvent(pointer('pointerup', y)));
const wait = (ms: number) => act(() => void vi.advanceTimersByTime(ms));

beforeEach(() => {
  renders = 0;
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  vi.useFakeTimers();
});
afterEach(() => {
  wait(COMMIT_WAIT_MS * 2);
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('useDrag: the row itself is carried', () => {
  it('renders nothing while the pointer moves: once when it is picked up, once when it is put down', () => {
    render(<List />);
    layOut();
    press('a', 20);
    moveTo(30);
    const picked = renders;
    expect(list().hasAttribute('data-carrying')).toBe(true);
    for (let y = 31; y < 140; y += 3) moveTo(y);
    expect(renders).toBe(picked);
    // The row followed the pointer the whole way: 139 - 20.
    expect(row('a').style.transform).toBe('translate3d(0, 119px, 0)');
  });

  it('keeps the row inside the list, held where it was taken', () => {
    render(<List />);
    layOut();
    press('b', 50);
    moveTo(60);
    moveTo(400);
    // The list ends at 160: the row (taken 10 px into it) goes no lower than 120, 80 below its place.
    expect(row('b').style.transform).toBe('translate3d(0, 80px, 0)');
    moveTo(-300);
    expect(row('b').style.transform).toBe('translate3d(0, -40px, 0)');
  });

  it('moves the neighbours aside only when its place changes', () => {
    render(<List />);
    layOut();
    press('a', 20);
    moveTo(30);
    expect(row('b').style.transform).toBe('');
    expect(row('b').hasAttribute('data-drag-shift')).toBe(true);
    moveTo(62);
    expect(row('b').style.transform).toBe('translate3d(0, -40px, 0)');
    expect(row('c').style.transform).toBe('');
    moveTo(102);
    expect(row('c').style.transform).toBe('translate3d(0, -40px, 0)');
    moveTo(70);
    expect(row('c').style.transform).toBe('');
  });

  it('settles the row into its place, then the order changes in one render and the transforms go with it', () => {
    const onDrop = vi.fn();
    render(<List onDrop={onDrop} />);
    layOut();
    press('a', 20);
    moveTo(30);
    moveTo(102);
    release(102);
    expect(row('a').style.transform).toBe('translate3d(0, 80px, 0)');
    expect(row('a').hasAttribute('data-drag-settle')).toBe(true);
    expect(onDrop).not.toHaveBeenCalled();
    expect(order()).toEqual(['a', 'b', 'c', 'd']);
    wait(SETTLE_MS);
    expect(onDrop).toHaveBeenCalledExactlyOnceWith({ id: 'a', to: 2 });
    expect(order()).toEqual(['b', 'c', 'a', 'd']);
    for (const id of ['a', 'b', 'c', 'd']) {
      expect(row(id).style.transform).toBe('');
      expect(row(id).hasAttribute('data-drag-shift')).toBe(false);
    }
    expect(row('a').hasAttribute('data-dragging')).toBe(false);
    expect(list().hasAttribute('data-carrying')).toBe(false);
  });

  it('Escape eases the row and its neighbours back, and drops nothing', () => {
    const onDrop = vi.fn();
    render(<List onDrop={onDrop} />);
    layOut();
    press('a', 20);
    moveTo(30);
    moveTo(102);
    act(() => void window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(row('a').style.transform).toBe('');
    expect(row('a').hasAttribute('data-drag-settle')).toBe(true);
    expect(row('b').style.transform).toBe('');
    release(102);
    wait(SETTLE_MS);
    expect(onDrop).not.toHaveBeenCalled();
    expect(order()).toEqual(['a', 'b', 'c', 'd']);
    expect(document.querySelector('[data-dragging], [data-drag-settle], [data-drag-shift]')).toBeNull();
  });

  it('puts everything down at once when motion is reduced', () => {
    window.matchMedia = ((query: string) => ({
      matches: query.includes('reduce'),
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    })) as unknown as typeof window.matchMedia;
    const onDrop = vi.fn();
    render(<List onDrop={onDrop} />);
    layOut();
    press('c', 100);
    moveTo(110);
    moveTo(20);
    release(20);
    expect(onDrop).toHaveBeenCalledExactlyOnceWith({ id: 'c', to: 0 });
    expect(order()).toEqual(['c', 'a', 'b', 'd']);
    expect(row('c').style.transform).toBe('');
  });
});
