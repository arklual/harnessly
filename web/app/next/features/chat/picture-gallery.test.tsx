// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { stepIndex, swipeOf } from '@/next/features/chat/picture-gallery';
import { PictureCards } from '@/next/features/chat/pictures';
import type { Picture } from '@/next/protocol/v2';

afterEach(cleanup);

vi.stubGlobal(
  'ResizeObserver',
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

const SIX: Picture[] = Array.from({ length: 6 }, (_, k) => ({
  id: `p${k + 1}`,
  text: `/tmp/shots/s${k + 1}.png`,
}));
const srcOf = (picture: Picture) => `/shot?id=${picture.id}`;
const hero = () => document.querySelector<HTMLImageElement>('.hx-gal__img');

describe('picture gallery', () => {
  it('steps round the ends', () => {
    expect(stepIndex(0, -1, 6)).toBe(5);
    expect(stepIndex(5, 1, 6)).toBe(0);
    expect(stepIndex(2, 1, 6)).toBe(3);
  });

  it('tells a swipe from a press and a scroll', () => {
    expect(swipeOf(-80, 5)).toBe(1);
    expect(swipeOf(80, 5)).toBe(-1);
    expect(swipeOf(10, 0)).toBeNull();
    expect(swipeOf(60, 120)).toBeNull();
  });

  it('shows one picture large with a counter, the arrows and the strip step through all of them', () => {
    render(<PictureCards pictures={SIX} srcOf={srcOf} big title="Скриншоты · 6" />);
    expect(document.querySelectorAll('.hx-gal__thumb')).toHaveLength(6);
    expect(document.querySelector('.hx-gal__count')?.textContent).toBe('1 / 6');
    expect(hero()?.getAttribute('src')).toBe('/shot?id=p1');
    fireEvent.click(document.querySelector('.hx-gal__arrow[data-dir="prev"]')!);
    expect(document.querySelector('.hx-gal__count')?.textContent).toBe('6 / 6');
    fireEvent.click(document.querySelectorAll('.hx-gal__thumb')[2]);
    expect(hero()?.getAttribute('src')).toBe('/shot?id=p3');
    expect(document.querySelectorAll('.hx-gal__thumb')[2].getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(document.querySelector('.hx-gal__stage')!, { key: 'ArrowRight' });
    expect(hero()?.getAttribute('src')).toBe('/shot?id=p4');
  });

  it('«Все» shows every picture in a grid, and a press there opens the viewer on it', () => {
    render(<PictureCards pictures={SIX} srcOf={srcOf} big title="Скриншоты · 6" />);
    fireEvent.click(document.querySelector('.hx-gal__all')!);
    const cells = document.querySelectorAll('.hx-galgrid__cell');
    expect(cells).toHaveLength(6);
    fireEvent.click(cells[4]);
    expect(hero()?.getAttribute('src')).toBe('/shot?id=p5');
    expect(screen.getAllByRole('dialog').length).toBeGreaterThanOrEqual(2);
    fireEvent.keyDown(window, { key: 'Escape' });
  });

  it('keeps the grid of a message’s own pictures when there is no title', () => {
    render(<PictureCards pictures={SIX} srcOf={srcOf} big />);
    expect(document.querySelector('.hx-gal')).toBeNull();
    expect(document.querySelectorAll('.hx-pic').length).toBe(5);
  });
});
