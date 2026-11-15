// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import ImageView, { SWIPE_PX } from '@/next/ui/image-view';

const ITEM = { src: 'a.png', name: 'a' };

beforeAll(() => {
  Element.prototype.setPointerCapture = () => {};
  // The picture is 400x300 at (100, 100): its centre is (300, 250).
  Element.prototype.getBoundingClientRect = function (this: Element) {
    const scale = Number((this as HTMLElement).style.scale || 1);
    const [x, y] = ((this as HTMLElement).style.translate || '0px 0px').split(' ').map(parseFloat);
    const [w, h] = [400 * scale, 300 * scale];
    return DOMRect.fromRect({ x: 300 + x - w / 2, y: 250 + y - h / 2, width: w, height: h });
  };
  // Its place in the layout, which its own translate and scale do not move (the zoom's centre is read from it).
  const layout = { offsetLeft: 100, offsetTop: 100, offsetWidth: 400, offsetHeight: 300 };
  for (const [key, value] of Object.entries(layout))
    Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
});
afterEach(cleanup);
beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

const setup = () => {
  const onStep = vi.fn();
  const view = render(
    <ImageView item={ITEM} onClose={() => {}} position={{ at: 1, of: 3 }} onStep={onStep} />,
  );
  const dialog = view.getByRole('dialog');
  const img = view.getByRole('img') as HTMLImageElement;
  return { onStep, dialog, img };
};
const swipe = (dialog: HTMLElement, dx: number) => {
  fireEvent.pointerDown(dialog, {
    pointerId: 1,
    pointerType: 'touch',
    clientX: 300,
    clientY: 250,
    button: 0,
  });
  fireEvent.pointerMove(dialog, { pointerId: 1, pointerType: 'touch', clientX: 300 + dx, clientY: 250 });
  fireEvent.pointerUp(dialog, { pointerId: 1, pointerType: 'touch', clientX: 300 + dx, clientY: 250 });
};

describe('ImageView zoom', () => {
  it('zooms with the wheel towards the cursor and keeps the point under it', () => {
    const { dialog, img } = setup();
    fireEvent.wheel(dialog, { deltaY: -300, clientX: 400, clientY: 250 });
    const scale = Number(img.style.scale);
    expect(scale).toBeGreaterThan(1);
    expect(dialog.dataset.zoomed).toBe('true');
    // The cursor was 100px right of the centre: the picture moves left by 100 * (scale - 1).
    expect(parseFloat(img.style.translate)).toBeCloseTo(-100 * (scale - 1), 3);
  });

  it('wheeling back out goes below fit to half at most, centred, and clears the zoom', () => {
    const { dialog, img } = setup();
    fireEvent.wheel(dialog, { deltaY: -300, clientX: 300, clientY: 250 });
    fireEvent.wheel(dialog, { deltaY: 3000, clientX: 300, clientY: 250, ctrlKey: true });
    expect(img.style.scale).toBe('0.5');
    expect(parseFloat(img.style.translate)).toBe(0);
    expect(dialog.dataset.zoomed).toBeUndefined();
  });

  it('a wheel without ctrl pans a zoomed picture (two fingers on a trackpad), with ctrl it zooms', () => {
    const { dialog, img } = setup();
    fireEvent.wheel(dialog, { deltaY: -300, clientX: 300, clientY: 250, ctrlKey: true });
    const scale = Number(img.style.scale);
    fireEvent.wheel(dialog, { deltaX: 30, deltaY: 0, clientX: 300, clientY: 250 });
    expect(Number(img.style.scale)).toBe(scale);
    expect(parseFloat(img.style.translate)).toBeCloseTo(-30, 3);
  });

  it('the buttons and the keys zoom, the percent goes to 100% and back', () => {
    const { dialog, img } = setup();
    const buttons = () => [...dialog.querySelectorAll<HTMLButtonElement>('.hx-imgview__zb')];
    act(() => vi.runAllTimers());
    const [out, pct, plus, fit] = buttons();
    expect(fit.disabled).toBe(true);
    act(() => plus.click());
    expect(Number(img.style.scale)).toBeCloseTo(1.5, 5);
    act(() => vi.runAllTimers());
    expect(buttons()[1].textContent).toBe('150%');
    act(() => out.click());
    expect(img.style.scale).toBe('');
    fireEvent.keyDown(dialog, { key: '+' });
    fireEvent.keyDown(dialog, { key: '+' });
    expect(Number(img.style.scale)).toBeCloseTo(2.25, 5);
    fireEvent.keyDown(dialog, { key: '0' });
    expect(img.style.scale).toBe('');
    // A picture of unknown size: its real size is the fitted one, so 100% zooms nowhere.
    act(() => pct.click());
    expect(img.style.scale).toBe('');
  });

  it('a double click zooms in and the next one is back to fit', () => {
    const { dialog, img } = setup();
    fireEvent.doubleClick(img, { clientX: 300, clientY: 250 });
    expect(Number(img.style.scale)).toBeGreaterThanOrEqual(2);
    fireEvent.doubleClick(img, { clientX: 300, clientY: 250 });
    expect(img.style.scale).toBe('');
    expect(dialog.dataset.zoomed).toBeUndefined();
  });

  it('pans by a drag while zoomed', () => {
    const { dialog, img } = setup();
    fireEvent.doubleClick(img, { clientX: 300, clientY: 250 });
    const before = parseFloat(img.style.translate);
    fireEvent.pointerDown(dialog, {
      pointerId: 1,
      pointerType: 'mouse',
      clientX: 300,
      clientY: 250,
      button: 0,
    });
    fireEvent.pointerMove(dialog, { pointerId: 1, pointerType: 'mouse', clientX: 340, clientY: 250 });
    expect(parseFloat(img.style.translate)).toBeCloseTo(before + 40, 3);
  });

  it('swipes to the next picture while fitted, not while zoomed', () => {
    const { dialog, img, onStep } = setup();
    swipe(dialog, -SWIPE_PX * 2);
    expect(onStep).toHaveBeenCalledWith(1);
    onStep.mockClear();
    fireEvent.wheel(dialog, { deltaY: -300, clientX: 300, clientY: 250 });
    expect(img.style.scale).not.toBe('');
    swipe(dialog, -SWIPE_PX * 2);
    expect(onStep).not.toHaveBeenCalled();
  });

  it('a new picture starts fitted', () => {
    const view = render(<ImageView item={ITEM} onClose={() => {}} />);
    fireEvent.wheel(view.getByRole('dialog'), { deltaY: -300, clientX: 300, clientY: 250 });
    expect(view.getByRole('dialog').dataset.zoomed).toBe('true');
    act(() => view.rerender(<ImageView item={{ src: 'b.png', name: 'b' }} onClose={() => {}} />));
    expect(view.getByRole('dialog').dataset.zoomed).toBeUndefined();
    expect((view.getByRole('img') as HTMLImageElement).style.scale).toBe('');
  });

  it('a click on the picture does not close it, a click on the scrim does', () => {
    const onClose = vi.fn();
    const view = render(<ImageView item={ITEM} onClose={onClose} />);
    fireEvent.click(view.getByRole('img'));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(view.getByRole('dialog'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
