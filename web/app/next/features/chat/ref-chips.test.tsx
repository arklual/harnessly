// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { REF_EXIT_MS, RefChips } from '@/next/features/chat/ref-chips';
import { onOpenAttached } from '@/next/features/pane/open-attached';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const design = { kind: 'design', id: 'd1', title: 'Checkout' } as const;
const file = { kind: 'file', id: 'src/a.ts', title: 'a.ts' } as const;

describe('RefChips', () => {
  it('opens the place in the pane when pressed', () => {
    const heard = vi.fn();
    const off = onOpenAttached(heard);
    render(<RefChips refs={[design]} />);
    fireEvent.click(screen.getByRole('button', { name: 'Open design “Checkout”' }));
    expect(heard).toHaveBeenCalledWith(expect.objectContaining({ kind: 'design', id: 'd1' }));
    off();
  });

  it('has a × in the field, and a removed chip leaves after its animation', () => {
    const onRemove = vi.fn();
    const { rerender } = render(<RefChips refs={[design, file]} onRemove={onRemove} />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove a.ts' }));
    expect(onRemove).toHaveBeenCalledWith(file);
    rerender(<RefChips refs={[design]} onRemove={onRemove} />);
    expect(document.querySelector('[data-ref="file"]')?.getAttribute('data-leaving')).toBe('true');
    act(() => vi.advanceTimersByTime(REF_EXIT_MS + 1));
    expect(document.querySelector('[data-ref="file"]')).toBeNull();
  });

  it('has no × outside the field', () => {
    render(<RefChips refs={[design]} />);
    expect(screen.queryByRole('button', { name: /Remove/ })).toBeNull();
  });
});
