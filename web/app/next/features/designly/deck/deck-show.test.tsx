// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DeckShow from './deck-show';
import type { IDeckPlayHost } from './deck-play';
import type { IDeckSlide } from './types';

const slide = (n: number, notes: string, seconds: number): IDeckSlide => ({
  id: `p${n}`,
  path: `s${n}.dc.html`,
  title: `Slide ${n}`,
  w: 1920,
  h: 1080,
  notes,
  voice: false,
  seconds,
  version: `v${n}`,
  blank: false,
});

const host = (over: Partial<IDeckPlayHost> = {}): IDeckPlayHost => ({
  slides: [slide(1, 'One. Two. Three.', 60), slide(2, 'Four.', 60), slide(3, '', 60)],
  index: 0,
  onGo: vi.fn(),
  steps: 0,
  step: 0,
  onStep: vi.fn(),
  slideRect: () => null,
  onPointer: () => () => {},
  setPointer: vi.fn(),
  frameSrc: () => null,
  ...over,
});

const stubMedia = (phone: boolean) =>
  vi.stubGlobal(
    'matchMedia',
    (query: string) =>
      ({
        matches: phone && query.includes('560'),
        addEventListener: () => {},
        removeEventListener: () => {},
      }) as unknown as MediaQueryList,
  );

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  stubMedia(false);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('DeckShow', () => {
  it('shows the clock, the next slide and the script with the first line current', () => {
    const { container } = render(<DeckShow host={host()} minutes={10} onExit={vi.fn()} onRecord={vi.fn()} />);
    expect(container.querySelector('.hx-deckm-clock__now')?.textContent).toBe('0:00');
    expect(container.querySelector('.hx-deckm-next')?.textContent).toContain('Slide 2');
    const lines = [...container.querySelectorAll('.hx-deckm-script p')];
    expect(lines.map((p) => p.getAttribute('data-on'))).toEqual(['1', '0', '0']);
    expect(container.querySelector('.hx-deckm-clock em')?.getAttribute('data-pace')).toBe('onTime');
  });

  it('counts the seconds and turns to «behind» past the plan of the slide', () => {
    const { container } = render(<DeckShow host={host()} minutes={3} onExit={vi.fn()} onRecord={vi.fn()} />);
    act(() => void vi.advanceTimersByTime(85_000));
    expect(container.querySelector('.hx-deckm-clock__now')?.textContent).toBe('1:25');
    expect(container.querySelector('.hx-deckm-clock em')?.getAttribute('data-pace')).toBe('behind');
  });

  it('has no next slide on the last one', () => {
    const { container } = render(
      <DeckShow host={host({ index: 2 })} minutes={10} onExit={vi.fn()} onRecord={vi.fn()} />,
    );
    expect(container.querySelector('.hx-deckm-next__mini')).toBeNull();
  });

  it('goes by the arrows: a step first, then the slide; Esc leaves; R records', () => {
    const h = host({ steps: 1 });
    const onExit = vi.fn();
    const onRecord = vi.fn();
    render(<DeckShow host={h} minutes={10} onExit={onExit} onRecord={onRecord} />);
    fireEvent.keyDown(window, { key: 'ArrowRight' });
    expect(h.onStep).toHaveBeenCalledWith(1);
    expect(h.onGo).not.toHaveBeenCalled();
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onExit).toHaveBeenCalled();
    fireEvent.keyDown(window, { key: 'r' });
    expect(onRecord).toHaveBeenCalled();
  });

  it('moves to the next slide when it has no steps left', () => {
    const h = host();
    render(<DeckShow host={h} minutes={10} onExit={vi.fn()} onRecord={vi.fn()} />);
    fireEvent.keyDown(window, { key: ' ' });
    expect(h.onGo).toHaveBeenCalledWith(1);
  });

  it('does not record by key on a phone', () => {
    stubMedia(true);
    const onRecord = vi.fn();
    render(<DeckShow host={host()} minutes={10} onExit={vi.fn()} onRecord={onRecord} />);
    fireEvent.keyDown(window, { key: 'r' });
    expect(onRecord).not.toHaveBeenCalled();
  });

  it('asks the board for the pointer only while the pointer is on', () => {
    const h = host();
    const { container } = render(<DeckShow host={h} minutes={10} onExit={vi.fn()} onRecord={vi.fn()} />);
    expect(h.setPointer).not.toHaveBeenCalledWith(true);
    fireEvent.click(container.querySelector('button[aria-pressed]') as Element);
    expect(h.setPointer).toHaveBeenCalledWith(true);
  });
});
