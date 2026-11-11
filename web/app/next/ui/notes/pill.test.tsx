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

  it('has «Annotate» alone when the host asks nothing aside', () => {
    setup(spot);
    expect(document.querySelectorAll('.hx-note__pillbtn')).toHaveLength(1);
  });

  it('puts «Ask aside» beside «Annotate» and hands its press to the host, keeping the words selected', () => {
    const onPress = vi.fn();
    const onAside = vi.fn();
    render(<AnnotatePill target={spot} onPress={onPress} onAside={onAside} onDismiss={vi.fn()} />);
    const buttons = document.querySelectorAll('.hx-note__pillbtn');
    expect(buttons).toHaveLength(2);
    const down = new PointerEvent('pointerdown', { bubbles: true, cancelable: true });
    buttons[1].dispatchEvent(down);
    expect(down.defaultPrevented).toBe(true);
    fireEvent.click(buttons[1]);
    expect(onAside).toHaveBeenCalledOnce();
    expect(onPress).not.toHaveBeenCalled();
  });

  it('presses its buttons with A and S when the host asks for keys, not while words are typed', () => {
    const onPress = vi.fn();
    const onAside = vi.fn();
    render(<AnnotatePill target={spot} onPress={onPress} onAside={onAside} keys onDismiss={vi.fn()} />);
    fireEvent.keyDown(document, { key: 's', code: 'KeyS' });
    expect(onAside).toHaveBeenCalledOnce();
    // A Russian layout: the key's place counts, not its letter.
    fireEvent.keyDown(document, { key: 'ф', code: 'KeyA' });
    expect(onPress).toHaveBeenCalledOnce();
    // With a modifier the key is someone else's.
    fireEvent.keyDown(document, { key: 's', code: 'KeyS', metaKey: true });
    const field = document.createElement('textarea');
    document.body.append(field);
    fireEvent.keyDown(field, { key: 'a', code: 'KeyA' });
    field.remove();
    expect(onAside).toHaveBeenCalledOnce();
    expect(onPress).toHaveBeenCalledOnce();
  });

  it('takes no keys unless the host asks for them', () => {
    const { onPress } = setup(spot);
    fireEvent.keyDown(document, { key: 'a', code: 'KeyA' });
    expect(onPress).not.toHaveBeenCalled();
  });

  it('stays through its exit, in the closing phase, then goes', () => {
    const { view, ...handlers } = setup(spot);
    view.rerender(<AnnotatePill target={null} {...handlers} />);
    expect(box()?.getAttribute('data-state')).toBe('closing');
    act(() => void vi.advanceTimersByTime(ANNOTATE_EXIT_MS));
    expect(box()).toBeNull();
  });
});
