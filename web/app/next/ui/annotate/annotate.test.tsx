// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Annotate, { ANNOTATE_EXIT_MS, type IAnnotateTarget } from '@/next/ui/annotate/annotate';

class FakeObserver {
  observe() {}
  disconnect() {}
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('ResizeObserver', FakeObserver);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const pill: IAnnotateTarget = { quote: 'the words', x: 300, top: 400, bottom: 420, expanded: false };
const card: IAnnotateTarget = { ...pill, expanded: true };

const setup = (target: IAnnotateTarget | null) => {
  const handlers = { onExpand: vi.fn(), onSubmit: vi.fn(), onDismiss: vi.fn() };
  const view = render(<Annotate target={target} {...handlers} />);
  return { ...handlers, view };
};

const box = () => document.querySelector('.hx-annot');
const field = () => document.querySelector<HTMLTextAreaElement>('.hx-annot__field');

describe('Annotate', () => {
  it('shows nothing without a target', () => {
    setup(null);
    expect(box()).toBeNull();
  });

  it('opens the card on a press of the pill', () => {
    const { onExpand } = setup(pill);
    expect(box()?.getAttribute('data-expanded')).toBe('false');
    fireEvent.click(document.querySelector('.hx-annot__pillbtn') as Element);
    expect(onExpand).toHaveBeenCalledOnce();
  });

  it('sends the remark on Enter and the words alone when there is none', () => {
    const { onSubmit } = setup(card);
    const input = field() as HTMLTextAreaElement;
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onSubmit).toHaveBeenLastCalledWith(null);
    fireEvent.change(input, { target: { value: '  fix this  ' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onSubmit).toHaveBeenLastCalledWith('fix this');
  });

  it('breaks the line on Shift+Enter instead of sending', () => {
    const { onSubmit } = setup(card);
    fireEvent.keyDown(field() as HTMLTextAreaElement, { key: 'Enter', shiftKey: true });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('folds away on Esc and on a press elsewhere, but not on a press inside', () => {
    const { onDismiss } = setup(card);
    fireEvent.pointerDown(box() as Element);
    expect(onDismiss).not.toHaveBeenCalled();
    fireEvent.keyDown(field() as HTMLTextAreaElement, { key: 'Escape' });
    expect(onDismiss).toHaveBeenCalledTimes(1);
    fireEvent.pointerDown(document.body);
    expect(onDismiss).toHaveBeenCalledTimes(2);
  });

  it('stays through its exit, in the closing phase, then goes', () => {
    const { view, ...handlers } = setup(card);
    view.rerender(<Annotate target={null} {...handlers} />);
    expect(box()?.getAttribute('data-state')).toBe('closing');
    act(() => void vi.advanceTimersByTime(ANNOTATE_EXIT_MS));
    expect(box()).toBeNull();
  });
});
