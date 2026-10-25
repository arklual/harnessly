// @vitest-environment jsdom
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Swap, { SWAP_MS } from '@/next/ui/swap';
import { SKELETON_DELAY_MS } from '@/next/ui/skeleton';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const view = (ready: boolean) => (
  <Swap ready={ready} skeleton={<i data-testid="sk" />}>
    <b data-testid="content" />
  </Swap>
);
const phase = (c: HTMLElement) => c.querySelector('.hx-swap')?.getAttribute('data-phase');

describe('Swap', () => {
  it('shows no skeleton before the delay, and the content with its own entrance when nothing was drawn before it', () => {
    const { container, queryByTestId, rerender } = render(view(false));
    expect(queryByTestId('sk')).toBeNull();
    rerender(view(true));
    expect(phase(container)).toBe('direct');
    expect(queryByTestId('content')).not.toBeNull();
    expect(queryByTestId('sk')).toBeNull();
  });

  it('after a drawn skeleton keeps both for the crossfade, then drops the skeleton; the content stays the same node', () => {
    const { container, queryByTestId, rerender } = render(view(false));
    act(() => void vi.advanceTimersByTime(SKELETON_DELAY_MS + 1));
    expect(queryByTestId('sk')).not.toBeNull();
    rerender(view(true));
    expect(phase(container)).toBe('swapping');
    const content = queryByTestId('content');
    expect(content).not.toBeNull();
    expect(queryByTestId('sk')).not.toBeNull();
    act(() => void vi.advanceTimersByTime(SWAP_MS + 1));
    expect(phase(container)).toBe('swapped');
    expect(queryByTestId('sk')).toBeNull();
    expect(queryByTestId('content')).toBe(content);
  });

  it('goes back to loading when the data is taken away', () => {
    const { container, rerender } = render(view(true));
    rerender(view(false));
    expect(phase(container)).toBe('loading');
  });

  it('keeps a mounted content unseen while it loads and crossfades it in without remounting', () => {
    const held = (ready: boolean) => (
      <Swap ready={ready} mounted skeleton={<i data-testid="sk" />}>
        <b data-testid="content" />
      </Swap>
    );
    const { container, queryByTestId, rerender } = render(held(false));
    const content = queryByTestId('content');
    expect(content).not.toBeNull();
    expect(content?.parentElement?.getAttribute('data-held')).toBe('true');
    act(() => void vi.advanceTimersByTime(SKELETON_DELAY_MS + 1));
    expect(queryByTestId('sk')).not.toBeNull();
    rerender(held(true));
    expect(phase(container)).toBe('swapping');
    expect(queryByTestId('content')).toBe(content);
    expect(content?.parentElement?.getAttribute('data-held')).toBe('false');
    act(() => void vi.advanceTimersByTime(SWAP_MS + 1));
    expect(queryByTestId('sk')).toBeNull();
  });
});
