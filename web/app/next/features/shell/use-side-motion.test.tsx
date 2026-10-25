// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import useSideMotion from '@/next/features/shell/use-side-motion';

interface IFake {
  keyframes: Keyframe[];
  cancel: ReturnType<typeof vi.fn>;
  finished: Promise<unknown>;
}

const fakes: IFake[] = [];

const setup = (media: Record<string, boolean> = {}) => {
  const app = document.createElement('div');
  app.style.setProperty('--hx-sidebar-w', '232px');
  app.style.setProperty('--hx-rail-w', '58px');
  const main = document.createElement('main');
  const body = document.createElement('div');
  main.append(body);
  app.append(main);
  document.body.append(app);
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({ matches: media[query] ?? false })),
  );
  const refs = { app: { current: app }, main: { current: main }, body: { current: body } };
  return { app, main, body, refs };
};

beforeEach(() => {
  fakes.length = 0;
  HTMLElement.prototype.animate = vi.fn((keyframes: Keyframe[]) => {
    const fake: IFake = { keyframes, cancel: vi.fn(), finished: Promise.resolve() };
    fakes.push(fake);
    return fake as unknown as Animation;
  });
  vi.stubGlobal(
    'DOMMatrixReadOnly',
    class {
      m41: number;
      constructor(value: string) {
        this.m41 = Number(/matrix\(1, 0, 0, 1, (-?[\d.]+),/.exec(value)?.[1] ?? 0);
      }
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

describe('useSideMotion', () => {
  it('slides nothing on the first render', () => {
    const { refs } = setup();
    renderHook(() => useSideMotion(refs, false));
    expect(fakes).toHaveLength(0);
  });

  it('folding slides the body and clips the window from where the edge was', () => {
    const { app, refs } = setup();
    const { rerender } = renderHook(({ rail }) => useSideMotion(refs, rail), {
      initialProps: { rail: false },
    });
    rerender({ rail: true });
    expect(fakes).toHaveLength(2);
    // The column went 174px to the left: the window's edge starts there and comes home.
    expect(fakes[0].keyframes).toEqual([
      { transform: 'translateX(174px)' },
      { transform: 'translateX(0px)' },
    ]);
    expect(fakes[1].keyframes).toEqual([
      { clipPath: 'inset(0 0 0 174px round 22px)' },
      { clipPath: 'inset(0 0 0 0px round 22px)' },
    ]);
    expect(app.dataset.move).toBe('on');
  });

  it('opening starts the other way round', () => {
    const { refs } = setup();
    const { rerender } = renderHook(({ rail }) => useSideMotion(refs, rail), {
      initialProps: { rail: true },
    });
    rerender({ rail: false });
    expect(fakes[0].keyframes[0]).toEqual({ transform: 'translateX(-174px)' });
    expect(fakes[1].keyframes[0]).toEqual({ clipPath: 'inset(0 0 0 -174px round 22px)' });
  });

  it('clears the mark when the slide is over', async () => {
    const { app, refs } = setup();
    const { rerender } = renderHook(({ rail }) => useSideMotion(refs, rail), {
      initialProps: { rail: false },
    });
    rerender({ rail: true });
    expect(app.dataset.move).toBe('on');
    await Promise.resolve();
    await Promise.resolve();
    expect(app.dataset.move).toBeUndefined();
  });

  it('a toggle in the middle starts from where the edge is and drops the old slide', () => {
    const { body, refs } = setup();
    const { rerender } = renderHook(({ rail }) => useSideMotion(refs, rail), {
      initialProps: { rail: false },
    });
    rerender({ rail: true });
    const first = fakes.slice();
    // Half way: the body is 87px off its place.
    vi.spyOn(window, 'getComputedStyle').mockImplementation(
      (el: Element) =>
        ({
          transform: el === body ? 'matrix(1, 0, 0, 1, 87, 0)' : '',
          getPropertyValue: (name: string) => (el as HTMLElement).style.getPropertyValue(name),
        }) as unknown as CSSStyleDeclaration,
    );
    rerender({ rail: false });
    expect(first[0].cancel).toHaveBeenCalled();
    expect(first[1].cancel).toHaveBeenCalled();
    expect(fakes[2].keyframes[0]).toEqual({ transform: 'translateX(-87px)' });
  });

  it('holds the right pane in place and takes the centred column half the way', () => {
    const { body, refs } = setup();
    const pane = document.createElement('aside');
    pane.className = 'hx-pane';
    const column = document.createElement('div');
    column.className = 'hx-feed__inner';
    body.append(pane, column);
    const targets: Element[] = [];
    HTMLElement.prototype.animate = vi.fn(function (this: HTMLElement, keyframes: Keyframe[]) {
      targets.push(this);
      const fake: IFake = { keyframes, cancel: vi.fn(), finished: Promise.resolve() };
      fakes.push(fake);
      return fake as unknown as Animation;
    });
    const { rerender } = renderHook(({ rail }) => useSideMotion(refs, rail), {
      initialProps: { rail: false },
    });
    rerender({ rail: true });
    const of = (el: Element) => fakes[targets.indexOf(el)].keyframes[0];
    expect(of(pane)).toEqual({ translate: '-174px 0' });
    expect(of(column)).toEqual({ translate: '-87px 0' });
    expect(of(body)).toEqual({ transform: 'translateX(174px)' });
  });

  it('keeps still when something is placed against the screen', () => {
    const { body, refs } = setup();
    const floating = document.createElement('div');
    floating.className = 'hx-sidechat';
    body.append(floating);
    const { rerender } = renderHook(({ rail }) => useSideMotion(refs, rail), {
      initialProps: { rail: false },
    });
    rerender({ rail: true });
    expect(fakes).toHaveLength(0);
  });

  it('does not slide for reduced motion or on the phone', () => {
    const reduced = setup({ '(prefers-reduced-motion: reduce)': true });
    const a = renderHook(({ rail }) => useSideMotion(reduced.refs, rail), { initialProps: { rail: false } });
    a.rerender({ rail: true });
    expect(fakes).toHaveLength(0);
    const phone = setup({ '(max-width: 900px)': true });
    const b = renderHook(({ rail }) => useSideMotion(phone.refs, rail), { initialProps: { rail: false } });
    b.rerender({ rail: true });
    expect(fakes).toHaveLength(0);
  });
});
