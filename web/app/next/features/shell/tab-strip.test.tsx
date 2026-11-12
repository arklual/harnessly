// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useRef, type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import TabStrip, { type IStripTab } from '@/next/features/shell/tab-strip';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';

const TABS: IStripTab[] = ['a', 'b', 'c', 'd'].map((id) => ({
  id,
  title: `Chat ${id}`,
  icon: 'message-square',
  busy: null,
  attention: false,
}));

const Host = ({ children }: { children: ReactNode }) => {
  const ref = useRef<HTMLDivElement>(null);
  return (
    <PortalContext value={ref}>
      {children}
      <div ref={ref} />
    </PortalContext>
  );
};

const handlers = () => ({
  onActivate: vi.fn(),
  onClose: vi.fn(),
  onCloseOthers: vi.fn(),
  onCloseRight: vi.fn(),
  onNew: vi.fn(),
  onMove: vi.fn(),
});

const mount = (tabs: readonly IStripTab[] = TABS) => {
  const on = handlers();
  render(
    <Host>
      <TabStrip tabs={tabs} active="a" {...on} />
    </Host>,
  );
  return on;
};

const tab = (id: string) => document.querySelector<HTMLElement>(`[data-tab="${id}"]`) as HTMLElement;
const list = () => document.querySelector<HTMLElement>('.hx-gtabs__list') as HTMLElement;

// jsdom lays nothing out: each tab is a 100 × 34 box side by side in a list of 400, and the pointer's place says which
// tab is under it.
const layOut = () => {
  const rect = (left: number, width: number, height: number) =>
    ({
      left,
      right: left + width,
      top: 0,
      bottom: height,
      width,
      height,
      x: left,
      y: 0,
      toJSON: () => ({}),
    }) as DOMRect;
  vi.spyOn(list(), 'getBoundingClientRect').mockReturnValue(rect(0, 400, 42));
  TABS.forEach((item, index) =>
    vi.spyOn(tab(item.id), 'getBoundingClientRect').mockReturnValue(rect(index * 100, 100, 34)),
  );
  document.elementFromPoint = (x: number) => tab(TABS[Math.max(0, Math.min(3, Math.floor(x / 100)))].id);
};

const pointer = (type: string, x: number, init: PointerEventInit = {}) =>
  new PointerEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: 10, pointerId: 1, ...init });

const press = (id: string, x: number, pointerType = 'mouse') =>
  act(
    () => void tab(id).dispatchEvent(pointer('pointerdown', x, { pointerType, button: 0, isPrimary: true })),
  );
// A move with the button held is drawn on the next frame.
const moveTo = (x: number, pointerType = 'mouse', init: PointerEventInit = {}) =>
  act(() => {
    window.dispatchEvent(pointer('pointermove', x, { pointerType, isPrimary: true, buttons: 1, ...init }));
    vi.advanceTimersByTime(16);
  });
/** A move with no button held: the release before it was not heard. */
const hoverTo = (x: number) => moveTo(x, 'mouse', { buttons: 0 });
/** The let-go tab eases into its place (or back to its own) before anything else happens. */
const settle = () => act(() => void vi.advanceTimersByTime(200));
/** A drop the owner did not follow with a new order: the tab goes back and the strip is free again. */
const giveUp = () => act(() => void vi.advanceTimersByTime(2000));
const release = (x: number, pointerType = 'mouse') =>
  act(() => void window.dispatchEvent(pointer('pointerup', x, { pointerType, isPrimary: true })));

/**
 * The end of an animation, with its name. jsdom has no AnimationEvent, so React listens for the prefixed event there.
 */
const animationEnd = (el: Element, animationName: string) =>
  act(() => {
    const event = new Event('webkitAnimationEnd', { bubbles: true });
    Object.defineProperty(event, 'animationName', { value: animationName });
    el.dispatchEvent(event);
  });

/** The wide window: the panels are popovers, not the phone's sheets. */
const wide = () => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
};

beforeEach(() => {
  wide();
  pinLocale('en');
  // jsdom has no scrolling.
  Element.prototype.scrollIntoView = () => undefined;
  vi.useFakeTimers();
});
afterEach(() => {
  // The click a drag eats is let through again a moment after it.
  act(() => void vi.advanceTimersByTime(10));
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('TabStrip: the menu of a tab', () => {
  it('opens at the pointer on a right click and closes the tab it was opened on', () => {
    const on = mount();
    fireEvent.contextMenu(tab('b'), { clientX: 120, clientY: 30 });
    expect(screen.getByRole('menu')).toBeTruthy();
    const names = screen.getAllByRole('menuitem').map((item) => item.textContent);
    expect(names).toEqual(['Close', 'Close other tabs', 'Close tabs to the right']);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Close' }));
    expect(on.onClose).toHaveBeenCalledWith('b');
    expect(on.onCloseOthers).not.toHaveBeenCalled();
  });

  it('closes the other tabs and the tabs to the right of the one it is of', () => {
    const on = mount();
    fireEvent.contextMenu(tab('c'), { clientX: 220, clientY: 30 });
    fireEvent.click(screen.getByRole('menuitem', { name: 'Close other tabs' }));
    expect(on.onCloseOthers).toHaveBeenCalledWith('c');
    cleanup();

    const again = mount();
    fireEvent.contextMenu(tab('b'), { clientX: 120, clientY: 30 });
    fireEvent.click(screen.getByRole('menuitem', { name: 'Close tabs to the right' }));
    expect(again.onCloseRight).toHaveBeenCalledWith('b');
  });

  it('has nothing to close to the right of the last tab, nor others for the only tab', () => {
    mount();
    fireEvent.contextMenu(tab('d'), { clientX: 320, clientY: 30 });
    expect(
      screen.getByRole('menuitem', { name: 'Close tabs to the right' }).hasAttribute('data-disabled'),
    ).toBe(true);
    cleanup();
    mount([TABS[0]]);
    fireEvent.contextMenu(tab('a'), { clientX: 20, clientY: 30 });
    expect(screen.getByRole('menuitem', { name: 'Close other tabs' }).hasAttribute('data-disabled')).toBe(
      true,
    );
  });

  it('opens from the keyboard under the tab, and Escape closes it', async () => {
    mount();
    tab('a').focus();
    fireEvent.keyDown(tab('a'), { key: 'F10', shiftKey: true });
    expect(screen.getByRole('menu')).toBeTruthy();
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    await act(async () => void (await vi.advanceTimersByTimeAsync(400)));
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('opens when a held finger is let go in place, and carries nothing', () => {
    const on = mount();
    layOut();
    press('b', 150, 'touch');
    act(() => void vi.advanceTimersByTime(400));
    expect(list().hasAttribute('data-dragging')).toBe(true);
    release(150, 'touch');
    // It opens after the click a touch ends with, or a moment later when none comes.
    expect(screen.queryByRole('menu')).toBeNull();
    act(() => void vi.advanceTimersByTime(500));
    expect(screen.getByRole('menu')).toBeTruthy();
    expect(on.onMove).not.toHaveBeenCalled();
    expect(list().hasAttribute('data-dragging')).toBe(false);
  });
});

describe('TabStrip: carrying a tab', () => {
  it('carries the tab itself with the pointer, the others stepping aside, and moves it once it has settled', () => {
    const on = mount();
    layOut();
    press('a', 50);
    moveTo(53);
    expect(list().hasAttribute('data-dragging')).toBe(false);
    moveTo(80);
    moveTo(275);
    expect(list().getAttribute('data-dragging')).toBe('tab');
    expect(tab('a').getAttribute('data-dragging')).toBe('true');
    // Held where it was taken: the pointer went 225 px along, so did the tab; b and c made room for it.
    expect(tab('a').style.transform).toBe('translate3d(225px, 0px, 0px)');
    expect(tab('b').style.transform).toBe('translate3d(-100px, 0px, 0px)');
    expect(tab('c').style.transform).toBe('translate3d(-100px, 0px, 0px)');
    expect(tab('d').style.transform).toBe('');
    expect(document.querySelector('.hx-dragghost')).toBeNull();
    release(275);
    expect(list().hasAttribute('data-dragging')).toBe(false);
    // It eases into the place of c, and only then does the order change.
    expect(tab('a').style.transform).toBe('translate3d(200px, 0px, 0px)');
    expect(on.onMove).not.toHaveBeenCalled();
    settle();
    expect(on.onMove).toHaveBeenCalledExactlyOnceWith('a', 2);
    expect(document.querySelector('[data-drop-over]')).toBeNull();
    giveUp();
    expect(tab('a').style.transform).toBe('');
    expect(tab('b').style.transform).toBe('');
    expect(document.querySelector('[data-dragging]')).toBeNull();
  });

  it('keeps the frames of the page off the pointer while the tab is carried, and gives it back on the drop', () => {
    mount();
    layOut();
    const root = document.documentElement;
    press('a', 50);
    moveTo(80);
    expect(root.getAttribute('data-hand-drag')).toBe('true');
    moveTo(275);
    release(275);
    expect(root.hasAttribute('data-hand-drag')).toBe(false);
    settle();
    giveUp();
    press('a', 50);
    moveTo(80);
    act(() => void window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(root.hasAttribute('data-hand-drag')).toBe(false);
    release(80);
  });

  it('grows a tab in once: a tab moved to another place does not play its entrance again', () => {
    const on = handlers();
    const { rerender } = render(
      <Host>
        <TabStrip tabs={TABS} active="a" {...on} />
      </Host>,
    );
    expect(tab('a').hasAttribute('data-enter')).toBe(true);
    for (const item of TABS) animationEnd(tab(item.id), 'hx-gtab-in');
    expect(document.querySelector('[data-enter]')).toBeNull();
    // An animation inside the tab (the icon of an agent at work) is not its entrance.
    const added: IStripTab = { ...TABS[0], id: 'e', title: 'Chat e' };
    rerender(
      <Host>
        <TabStrip tabs={[...TABS, added]} active="a" {...on} />
      </Host>,
    );
    animationEnd(tab('e').querySelector('.hx-gtab__ic') as Element, 'hx-spin');
    expect(tab('e').hasAttribute('data-enter')).toBe(true);
    // The new order (a drop on c) moves a's very element: it comes without an entrance.
    const [a, b, c, d] = TABS;
    rerender(
      <Host>
        <TabStrip tabs={[b, c, a, d, added]} active="a" {...on} />
      </Host>,
    );
    expect(tab('a').hasAttribute('data-enter')).toBe(false);
  });

  it('drops at the end when the pointer is past the last tab, and at the start before the first', () => {
    const on = mount();
    layOut();
    document.elementFromPoint = () => list();
    press('b', 150);
    moveTo(200);
    moveTo(395);
    release(395);
    settle();
    expect(on.onMove).toHaveBeenLastCalledWith('b', 3);
    giveUp();
    press('c', 250);
    moveTo(200);
    moveTo(2);
    release(2);
    settle();
    expect(on.onMove).toHaveBeenLastCalledWith('c', 0);
  });

  it('moves nothing when the tab is let go where it was', () => {
    const on = mount();
    layOut();
    press('b', 150);
    moveTo(170);
    moveTo(120);
    release(120);
    settle();
    expect(on.onMove).not.toHaveBeenCalled();
    expect(list().hasAttribute('data-dragging')).toBe(false);
    expect(tab('b').style.transform).toBe('');
  });

  it('leaves nothing picked up after Escape, a cancelled pointer or a lost window', () => {
    const on = mount();
    layOut();
    const stuck = () =>
      list().hasAttribute('data-dragging') || document.querySelector('[data-dragging], [data-drop-over]');

    press('a', 50);
    moveTo(80);
    moveTo(275);
    expect(list().hasAttribute('data-dragging')).toBe(true);
    act(() => void window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    // It eases back to its own place.
    expect(tab('a').style.transform).toBe('');
    settle();
    expect(stuck()).toBeFalsy();
    release(275);

    press('a', 50);
    moveTo(80);
    moveTo(275);
    act(() => void window.dispatchEvent(pointer('pointercancel', 275, { pointerType: 'mouse' })));
    settle();
    expect(stuck()).toBeFalsy();

    press('a', 50);
    moveTo(80);
    moveTo(275);
    act(() => void window.dispatchEvent(new Event('blur')));
    settle();
    expect(stuck()).toBeFalsy();
    release(275);

    expect(on.onMove).not.toHaveBeenCalled();
  });

  it('ends the drag on the release: the tab does not follow the pointer after it', () => {
    const on = mount();
    layOut();
    press('a', 50);
    moveTo(80);
    moveTo(275);
    release(275);
    settle();
    expect(on.onMove).toHaveBeenCalledTimes(1);
    expect(list().hasAttribute('data-dragging')).toBe(false);
    giveUp();
    // The pointer goes on, with the button held or not: nothing is carried any more.
    moveTo(390);
    hoverTo(10);
    expect(list().hasAttribute('data-dragging')).toBe(false);
    expect(document.querySelector('[data-dragging]')).toBeNull();
    expect(tab('a').style.transform).toBe('');
    expect(on.onMove).toHaveBeenCalledTimes(1);
  });

  it('puts the tab down on the first move without a button when the release was not heard', () => {
    const on = mount();
    layOut();
    press('a', 50);
    moveTo(80);
    moveTo(275);
    // Let go over the window's frame: no pointerup; the next move comes with no button held.
    hoverTo(390);
    expect(list().hasAttribute('data-dragging')).toBe(false);
    settle();
    // Put down where it was last carried to, not where the free pointer is now.
    expect(on.onMove).toHaveBeenLastCalledWith('a', 2);
    giveUp();
    hoverTo(10);
    expect(document.querySelector('[data-dragging]')).toBeNull();
    expect(tab('a').style.transform).toBe('');
  });

  it('picks nothing up when the release of a press was not heard before the pointer moved on', () => {
    const on = mount();
    layOut();
    press('b', 150);
    hoverTo(300);
    hoverTo(20);
    expect(list().hasAttribute('data-dragging')).toBe(false);
    expect(tab('b').style.transform).toBe('');
    // The next press works as one.
    press('c', 250);
    release(250);
    expect(on.onActivate).toHaveBeenLastCalledWith('c');
    expect(on.onMove).not.toHaveBeenCalled();
  });

  it('switches on a click without a move and carries nothing', () => {
    const on = mount();
    layOut();
    press('c', 250);
    moveTo(252);
    release(252);
    fireEvent.click(tab('c'));
    expect(on.onActivate).toHaveBeenCalledWith('c');
    expect(on.onMove).not.toHaveBeenCalled();
    expect(list().hasAttribute('data-dragging')).toBe(false);
    expect(tab('c').style.transform).toBe('');
  });

  it('ends the drag when the pointer leaves the window or the list loses it', () => {
    const on = mount();
    layOut();
    const stuck = () =>
      list().hasAttribute('data-dragging') || document.querySelector('[data-dragging], [data-drop-over]');

    press('a', 50);
    moveTo(80);
    moveTo(275);
    moveTo(275, 'mouse', { clientY: -200 });
    settle();
    expect(stuck()).toBeFalsy();
    expect(tab('a').style.transform).toBe('');
    release(275);

    press('a', 50);
    moveTo(80);
    moveTo(275);
    act(() => void list().dispatchEvent(pointer('lostpointercapture', 275, { pointerType: 'mouse' })));
    settle();
    expect(stuck()).toBeFalsy();
    moveTo(300);
    expect(stuck()).toBeFalsy();
    release(300);

    // Out of the window before the pickup: nothing is picked up.
    press('b', 150);
    act(() => void document.documentElement.dispatchEvent(new MouseEvent('mouseleave')));
    moveTo(300);
    expect(stuck()).toBeFalsy();
    release(300);

    expect(on.onMove).not.toHaveBeenCalled();
  });

  it('switches on the press of a mouse and on the tap of a finger, not on the press of one', () => {
    const on = mount();
    layOut();
    press('c', 250, 'touch');
    expect(on.onActivate).not.toHaveBeenCalled();
    fireEvent.click(tab('c'));
    expect(on.onActivate).toHaveBeenCalledWith('c');
    on.onActivate.mockClear();
    press('b', 150, 'mouse');
    expect(on.onActivate).toHaveBeenCalledWith('b');
  });
});

describe('TabStrip: the keys', () => {
  it('moves the tab one place with Alt and the arrows', () => {
    const on = mount();
    fireEvent.keyDown(tab('b'), { key: 'ArrowRight', altKey: true });
    expect(on.onMove).toHaveBeenCalledWith('b', 2);
    fireEvent.keyDown(tab('a'), { key: 'ArrowLeft', altKey: true });
    expect(on.onMove).toHaveBeenCalledTimes(1);
  });
});
