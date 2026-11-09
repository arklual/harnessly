// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AnnotatePill, { ANNOTATE_EXIT_MS } from '@/next/ui/notes/pill';
import type { IAnnotateSpot } from '@/next/ui/notes/place';

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const spot: IAnnotateSpot = { x: 300, top: 400, bottom: 420 };

const setup = (target: IAnnotateSpot | null) => {
  const handlers = { onPress: vi.fn(), onDismiss: vi.fn() };
  const view = render(<AnnotatePill target={target} {...handlers} />);
  return { ...handlers, view };
};

const box = () => document.querySelector('.hx-note__pill');
const button = () => document.querySelector('.hx-note__pillbtn') as Element;

describe('AnnotatePill', () => {
  it('shows nothing without a target', () => {
    setup(null);
    expect(box()).toBeNull();
  });

  it('hands the press to the host', () => {
    const { onPress } = setup(spot);
    fireEvent.click(button());
    expect(onPress).toHaveBeenCalledOnce();
  });

  it('keeps the picked words selected when pressed', () => {
    setup(spot);
    const down = new PointerEvent('pointerdown', { bubbles: true, cancelable: true });
    button().dispatchEvent(down);
    expect(down.defaultPrevented).toBe(true);
  });

  it('folds away on Esc and on a press elsewhere, but not on a press on itself', () => {
    const { onDismiss } = setup(spot);
    fireEvent.pointerDown(button());
    expect(onDismiss).not.toHaveBeenCalled();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onDismiss).toHaveBeenCalledTimes(1);
    fireEvent.pointerDown(document.body);
    expect(onDismiss).toHaveBeenCalledTimes(2);
  });

  it('stays through its exit, in the closing phase, then goes', () => {
    const { view, ...handlers } = setup(spot);
    view.rerender(<AnnotatePill target={null} {...handlers} />);
    expect(box()?.getAttribute('data-state')).toBe('closing');
    act(() => void vi.advanceTimersByTime(ANNOTATE_EXIT_MS));
    expect(box()).toBeNull();
  });
});
