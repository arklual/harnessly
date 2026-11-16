// @vitest-environment jsdom
// The pictures strip: four squares and «Ещё N», a skeleton while the snapshot is awaited, and a viewer that says who
// looked at the picture and when, steps by the side buttons, the arrows and a swipe, and closes with an exit.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import ShotsStrip, { SHOTS_HEAD } from '@/next/features/background/shots-strip';
import { IMAGE_VIEW_EXIT_MS } from '@/next/ui/image-view';
import type { Ms, Shot } from '@/next/protocol/v2';

beforeAll(() => {
  pinLocale('en');
  // The viewer's picture canvas (image-edit's zoom) fits itself by a ResizeObserver, which jsdom has not.
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  );
});
beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const NOW = 100_000_000;
const MIN = 60_000;

const shots = (n: number): Shot[] =>
  Array.from({ length: n }, (_, i) => ({
    id: `s${i}`,
    name: `page-${i}.png`,
    at: (NOW - (i + 1) * 5 * MIN) as Ms,
    ...(i === 1 ? { agent: 'Check the phone' } : {}),
  }));

const draw = (list: Shot[] | null) =>
  render(<ShotsStrip shots={list} srcOf={(s) => `/shot/${s.id}`} now={NOW} />);

const squares = (c: HTMLElement) => c.querySelectorAll('.hx-shots__sq');

describe('ShotsStrip', () => {
  it('shows the first four squares and the rest behind «N more»', () => {
    const { container } = draw(shots(7));
    expect(screen.getByRole('region', { name: 'Screenshots' })).toBeTruthy();
    expect(container.querySelector('.hx-shots__grid')?.children).toHaveLength(SHOTS_HEAD);
    const more = screen.getByRole('button', { name: /3 more/ });
    expect(more.getAttribute('aria-expanded')).toBe('false');
    expect(container.querySelector('.hx-shots__rest-clip')?.hasAttribute('inert')).toBe(true);
    fireEvent.click(more);
    expect(container.querySelector('.hx-shots__rest-clip')?.hasAttribute('inert')).toBe(false);
    expect(squares(container)).toHaveLength(7);
  });

  it('has no «more» for a few pictures and draws nothing for none', () => {
    const { container, rerender } = draw(shots(3));
    expect(screen.queryByRole('button', { name: /more/ })).toBeNull();
    rerender(<ShotsStrip shots={[]} srcOf={() => ''} now={NOW} />);
    act(() => void vi.advanceTimersByTime(400));
    expect(container.querySelector('.hx-shots')).toBeNull();
  });

  it('hides a picture that failed to load', () => {
    const { container } = draw(shots(3));
    fireEvent.error(container.querySelectorAll('img')[0]);
    expect(squares(container)).toHaveLength(2);
  });

  it('stands a skeleton in until the snapshot has come, and none before it is slow', () => {
    const { container } = draw(null);
    expect(container.querySelector('.hx-shots')).toBeNull();
    act(() => void vi.advanceTimersByTime(200));
    expect(container.querySelectorAll('.hx-sk')).toHaveLength(4);
  });
});

// The viewer opens with the marks' tools on: the pen draws, so there is no caption and no swipe; the
// pictures step by the side buttons and the arrows.
describe('the viewer of the pictures', () => {
  const open = (container: HTMLElement, index: number) => fireEvent.click(squares(container)[index]);
  const dialog = () => screen.getByRole('dialog');
  const shown = () => dialog().querySelector('img')?.getAttribute('src');

  it('opens on the picture it was clicked on', () => {
    const { container } = draw(shots(3));
    open(container, 1);
    expect(shown()).toBe('/shot/s1');
  });

  it('steps by the arrows and the side buttons, round the ends', () => {
    const { container } = draw(shots(3));
    open(container, 0);
    expect(shown()).toBe('/shot/s0');
    fireEvent.keyDown(document, { key: 'ArrowRight' });
    expect(shown()).toBe('/shot/s1');
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(shown()).toBe('/shot/s0');
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(shown()).toBe('/shot/s2');
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(shown()).toBe('/shot/s0');
  });

  it('closes by Escape with a closing phase', () => {
    const { container } = draw(shots(3));
    open(container, 0);
    expect(dialog().getAttribute('data-state')).toBe('open');
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(dialog().getAttribute('data-state')).toBe('closing');
    act(() => void vi.advanceTimersByTime(IMAGE_VIEW_EXIT_MS));
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
