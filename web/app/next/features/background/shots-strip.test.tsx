// @vitest-environment jsdom
// The pictures strip: four squares and «Ещё N», a skeleton while the snapshot is awaited, and a viewer that says who
// looked at the picture and when, steps by the side buttons, the arrows and a swipe, and closes with an exit.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import ShotsStrip, { SHOTS_HEAD } from '@/next/features/background/shots-strip';
import { IMAGE_VIEW_EXIT_MS, SWIPE_PX } from '@/next/ui/image-view';
import type { Ms, Shot } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));
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

  it('stands a skeleton in until the snapshot has come, and none before it is slow', () => {
    const { container } = draw(null);
    expect(container.querySelector('.hx-shots')).toBeNull();
    act(() => void vi.advanceTimersByTime(200));
    expect(container.querySelectorAll('.hx-sk')).toHaveLength(4);
  });
});

describe('the viewer of the pictures', () => {
  const open = (container: HTMLElement, index: number) => fireEvent.click(squares(container)[index]);
  const dialog = () => screen.getByRole('dialog');

  it('says which agent looked at the picture and when', () => {
    const { container } = draw(shots(3));
    open(container, 1);
    expect(dialog().textContent).toContain('page-1.png');
    expect(dialog().textContent).toContain('Check the phone · 10 min');
    expect(dialog().textContent).toContain('2 / 3');
    expect(dialog().querySelector('img')?.getAttribute('src')).toBe('/shot/s1');
  });

  it('steps by the arrows, the side buttons and a swipe, round the ends', () => {
    const { container } = draw(shots(3));
    open(container, 0);
    expect(dialog().textContent).toContain('1 / 3');
    fireEvent.keyDown(document, { key: 'ArrowRight' });
    expect(dialog().textContent).toContain('2 / 3');
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(dialog().textContent).toContain('1 / 3');
    // A finger moving left by SWIPE_PX steps on; mostly vertical movement does not.
    fireEvent.pointerDown(dialog(), { clientX: 200, clientY: 300 });
    fireEvent.pointerUp(dialog(), { clientX: 200 - SWIPE_PX - 10, clientY: 310 });
    expect(dialog().textContent).toContain('2 / 3');
    fireEvent.pointerDown(dialog(), { clientX: 200, clientY: 300 });
    fireEvent.pointerUp(dialog(), { clientX: 130, clientY: 480 });
    expect(dialog().textContent).toContain('2 / 3');
    // A swipe right goes back, and past the first picture to the last.
    fireEvent.pointerDown(dialog(), { clientX: 100, clientY: 300 });
    fireEvent.pointerUp(dialog(), { clientX: 100 + SWIPE_PX + 10, clientY: 300 });
    fireEvent.pointerDown(dialog(), { clientX: 100, clientY: 300 });
    fireEvent.pointerUp(dialog(), { clientX: 100 + SWIPE_PX + 10, clientY: 300 });
    expect(dialog().textContent).toContain('3 / 3');
  });

  it('closes with a closing phase, and a swipe is not a tap on the scrim', () => {
    const { container } = draw(shots(3));
    open(container, 0);
    fireEvent.pointerDown(dialog(), { clientX: 200, clientY: 300 });
    fireEvent.pointerUp(dialog(), { clientX: 100, clientY: 300 });
    fireEvent.click(dialog());
    expect(dialog().getAttribute('data-state')).toBe('open');
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(dialog().getAttribute('data-state')).toBe('closing');
    act(() => void vi.advanceTimersByTime(IMAGE_VIEW_EXIT_MS));
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
