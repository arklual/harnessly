// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import usePaneMotion, { paneExitMs } from '@/next/features/shell/use-pane-motion';

interface IFake {
  target: HTMLElement;
  keyframes: Keyframe[];
  options: KeyframeAnimationOptions;
  cancel: ReturnType<typeof vi.fn>;
  playState: string;
}

const fakes: IFake[] = [];

const setup = (media: Record<string, boolean> = {}) => {
  const stage = document.createElement('div');
  stage.className = 'hx-main__body';
  const feed = document.createElement('div');
  feed.className = 'hx-feed';
  const dock = document.createElement('div');
  dock.className = 'hx-dock';
  const side = document.createElement('div');
  side.className = 'hx-side';
  const pane = document.createElement('aside');
  pane.className = 'hx-pane';
  Object.defineProperty(pane, 'offsetWidth', { value: 400 });
  pane.style.setProperty('right', '12px');
  stage.append(feed, dock, side, pane);
  document.body.append(stage);
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({ matches: media[query] ?? false })),
  );
  // The probe sized by --pane-w measures as the pane does.
  Object.defineProperty(HTMLElement.prototype, 'offsetWidth', {
    configurable: true,
    get() {
      return this === pane || this.style?.getPropertyValue('width') === 'var(--pane-w)' ? 400 : 0;
    },
  });
  return { stage, feed, dock, side, pane, ref: { current: pane } };
};

beforeEach(() => {
  fakes.length = 0;
  HTMLElement.prototype.animate = vi.fn(function (this: HTMLElement, keyframes: Keyframe[], options) {
    const fake: IFake = {
      target: this,
      keyframes,
      options: options as KeyframeAnimationOptions,
      cancel: vi.fn(),
      playState: 'running',
    };
    fakes.push(fake);
    return fake as unknown as Animation;
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

describe('usePaneMotion', () => {
  it('moves nothing on the first render', () => {
    const { ref } = setup();
    renderHook(() => usePaneMotion(ref, true));
    expect(fakes).toHaveLength(0);
  });

  it('opening slides the pane in from beyond the stage and brings the chat from where it stood', () => {
    const { ref, feed, dock, side, pane } = setup();
    const { rerender } = renderHook(({ open }) => usePaneMotion(ref, open), {
      initialProps: { open: false },
    });
    rerender({ open: true });
    const of = (el: HTMLElement) => fakes.find((x) => x.target === el);
    // 400 wide and 12 from the edge: 412 to travel.
    expect(of(pane)?.keyframes).toEqual([
      { transform: 'translateX(412px)' },
      { transform: 'translateX(0px)' },
    ]);
    expect(of(feed)?.keyframes[0]).toEqual({ translate: '206px 0' });
    expect(of(dock)?.keyframes[0]).toEqual({ translate: '206px 0' });
    expect(of(side)?.keyframes[0]).toEqual({ translate: '412px 0' });
    expect(of(pane)?.options).toMatchObject({ duration: 240, easing: 'cubic-bezier(0.22, 1, 0.36, 1)' });
  });

  it('closing sends the pane out and starts the chat the other way round', () => {
    const { ref, feed, pane } = setup();
    const { rerender } = renderHook(({ open }) => usePaneMotion(ref, open), { initialProps: { open: true } });
    rerender({ open: false });
    const of = (el: HTMLElement) => fakes.find((x) => x.target === el);
    expect(of(pane)?.keyframes).toEqual([
      { transform: 'translateX(0px)' },
      { transform: 'translateX(412px)' },
    ]);
    expect(of(pane)?.options.fill).toBe('forwards');
    expect(of(feed)?.keyframes[0]).toEqual({ translate: '-206px 0' });
  });

  it('a toggle in the middle cancels the slide that runs', () => {
    const { ref } = setup();
    const { rerender } = renderHook(({ open }) => usePaneMotion(ref, open), {
      initialProps: { open: false },
    });
    rerender({ open: true });
    const first = [...fakes];
    rerender({ open: false });
    for (const slide of first) expect(slide.cancel).toHaveBeenCalled();
  });

  it('skips the slide on the phone and with reduced motion', () => {
    const phone = setup({ '(max-width: 900px)': true });
    const a = renderHook(({ open }) => usePaneMotion(phone.ref, open), { initialProps: { open: false } });
    a.rerender({ open: true });
    expect(fakes).toHaveLength(0);
    a.unmount();
    document.body.replaceChildren();
    const calm = setup({ '(prefers-reduced-motion: reduce)': true });
    const b = renderHook(({ open }) => usePaneMotion(calm.ref, open), { initialProps: { open: false } });
    b.rerender({ open: true });
    expect(fakes).toHaveLength(0);
  });

  it('keeps the closing pane as long as its way out takes', () => {
    setup();
    expect(paneExitMs()).toBe(240);
    setup({ '(max-width: 900px)': true });
    expect(paneExitMs()).toBe(300);
    setup({ '(prefers-reduced-motion: reduce)': true });
    expect(paneExitMs()).toBe(0);
  });
});
