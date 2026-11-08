// @vitest-environment jsdom
// SPDX-License-Identifier: Apache-2.0
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { VizLegend, type VizLegendItem } from './viz-legend';

type Id = 'dance' | 'vip' | 'balcony';

const ITEMS: VizLegendItem<Id>[] = [
  { id: 'dance', label: 'Танцпол', color: 'rgb(109, 73, 244)' },
  { id: 'vip', label: 'VIP', color: 'rgb(43, 127, 255)' },
  { id: 'balcony', label: 'Балкон', color: 'rgb(0, 160, 120)' },
];

const button = (name: string) => screen.getByRole('button', { name });

describe('VizLegend', () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  describe('rendering', () => {
    it('renders one button per series, pressed while the series is shown', () => {
      render(<VizLegend items={ITEMS} hidden={new Set<Id>(['vip'])} onToggle={() => undefined} />);
      expect(screen.getAllByRole('button')).toHaveLength(3);
      expect(button('Танцпол').getAttribute('aria-pressed')).toBe('true');
      expect(button('VIP').getAttribute('aria-pressed')).toBe('false');
      expect(button('Балкон').getAttribute('aria-pressed')).toBe('true');
    });

    it('marks hidden items for styling', () => {
      render(<VizLegend items={ITEMS} hidden={new Set<Id>(['vip'])} onToggle={() => undefined} />);
      expect(button('VIP').getAttribute('data-off')).toBe('true');
      expect(button('Танцпол').hasAttribute('data-off')).toBe(false);
    });

    it('wraps items in a labelled group', () => {
      render(<VizLegend items={ITEMS} onToggle={() => undefined} aria-label="Серии" />);
      expect(screen.getByRole('group', { name: 'Серии' })).not.toBeNull();
    });

    it('has a default group label', () => {
      render(<VizLegend items={ITEMS} onToggle={() => undefined} />);
      expect(screen.getByRole('group', { name: 'Легенда' })).not.toBeNull();
    });

    it('highlights the focused series and dims the rest via the group class', () => {
      render(<VizLegend items={ITEMS} focus="vip" onToggle={() => undefined} />);
      expect(screen.getByRole('group').classList.contains('viz-legend--focused')).toBe(true);
      expect(button('VIP').getAttribute('data-focus')).toBe('true');
      expect(button('Танцпол').hasAttribute('data-focus')).toBe(false);
    });

    it('renders plain text, not buttons, when there are no handlers', () => {
      render(<VizLegend items={ITEMS} />);
      expect(screen.queryByRole('button')).toBeNull();
      expect(screen.getByText('Танцпол')).not.toBeNull();
      expect(screen.getByText('VIP')).not.toBeNull();
    });

    it('applies a custom class name', () => {
      render(<VizLegend items={ITEMS} onToggle={() => undefined} className="custom" />);
      expect(screen.getByRole('group').classList.contains('custom')).toBe(true);
    });
  });

  describe('single series', () => {
    it('renders nothing for one item', () => {
      const { container } = render(<VizLegend items={[ITEMS[0]]} onToggle={() => undefined} />);
      expect(container.childElementCount + (container.textContent?.length ?? 0)).toBe(0);
    });

    it('renders nothing for no items', () => {
      const { container } = render(<VizLegend items={[]} onToggle={() => undefined} />);
      expect(container.childElementCount + (container.textContent?.length ?? 0)).toBe(0);
    });

    it('renders one item when `always` is set', () => {
      render(<VizLegend items={[ITEMS[0]]} always onToggle={() => undefined} />);
      expect(button('Танцпол')).not.toBeNull();
    });
  });

  describe('loading', () => {
    it('renders a hidden skeleton instead of items', () => {
      const { container } = render(<VizLegend items={ITEMS} loading onToggle={() => undefined} />);
      expect(screen.queryByRole('button')).toBeNull();
      expect(screen.queryByText('Танцпол')).toBeNull();
      const root = container.firstElementChild as HTMLElement;
      expect(root.classList.contains('viz-legend--loading')).toBe(true);
      expect(root.getAttribute('aria-hidden')).toBe('true');
      expect(root.querySelectorAll('.viz-legend__skeleton')).toHaveLength(5);
    });

    it('shows the skeleton even with no items yet', () => {
      const { container } = render(<VizLegend items={[]} loading />);
      expect(container.querySelector('.viz-legend--loading')).not.toBeNull();
    });
  });

  describe('click', () => {
    it('keyboard activation (detail 0) toggles without the double-click wait', () => {
      vi.useFakeTimers();
      const onToggle = vi.fn();
      const onSolo = vi.fn();
      render(<VizLegend items={ITEMS} onToggle={onToggle} onSolo={onSolo} />);
      fireEvent.click(button('VIP'), { detail: 0 });
      act(() => {
        vi.advanceTimersByTime(0);
      });
      expect(onToggle).toHaveBeenCalledTimes(1);
      expect(onToggle).toHaveBeenCalledWith('vip');
    });

    it('mouse click waits for a possible double click when onSolo is present', () => {
      vi.useFakeTimers();
      const onToggle = vi.fn();
      render(<VizLegend items={ITEMS} onToggle={onToggle} onSolo={() => undefined} />);
      fireEvent.click(button('VIP'), { detail: 1 });
      act(() => {
        vi.advanceTimersByTime(199);
      });
      expect(onToggle).not.toHaveBeenCalled();
      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(onToggle).toHaveBeenCalledTimes(1);
      expect(onToggle).toHaveBeenCalledWith('vip');
    });

    it('mouse click toggles at once when there is no onSolo', () => {
      vi.useFakeTimers();
      const onToggle = vi.fn();
      render(<VizLegend items={ITEMS} onToggle={onToggle} />);
      fireEvent.click(button('VIP'), { detail: 1 });
      act(() => {
        vi.advanceTimersByTime(0);
      });
      expect(onToggle).toHaveBeenCalledWith('vip');
    });

    it('a repeated fast click does not toggle twice', () => {
      vi.useFakeTimers();
      const onToggle = vi.fn();
      render(<VizLegend items={ITEMS} onToggle={onToggle} onSolo={() => undefined} />);
      fireEvent.click(button('VIP'), { detail: 1 });
      fireEvent.click(button('VIP'), { detail: 2 });
      act(() => {
        vi.advanceTimersByTime(1000);
      });
      expect(onToggle).not.toHaveBeenCalled();
    });
  });

  describe('double click', () => {
    it('calls onSolo and does not toggle', () => {
      vi.useFakeTimers();
      const onToggle = vi.fn();
      const onSolo = vi.fn();
      render(<VizLegend items={ITEMS} onToggle={onToggle} onSolo={onSolo} />);
      const target = button('Балкон');
      fireEvent.click(target, { detail: 1 });
      fireEvent.click(target, { detail: 2 });
      fireEvent.doubleClick(target);
      act(() => {
        vi.advanceTimersByTime(1000);
      });
      expect(onSolo).toHaveBeenCalledTimes(1);
      expect(onSolo).toHaveBeenCalledWith('balcony');
      expect(onToggle).not.toHaveBeenCalled();
    });

    it('works with only onSolo given', () => {
      const onSolo = vi.fn();
      render(<VizLegend items={ITEMS} onSolo={onSolo} />);
      fireEvent.doubleClick(button('VIP'));
      expect(onSolo).toHaveBeenCalledWith('vip');
    });
  });

  describe('hover and focus', () => {
    it('reports the series on enter and null on leave', () => {
      const onFocus = vi.fn();
      render(<VizLegend items={ITEMS} onToggle={() => undefined} onFocus={onFocus} />);
      fireEvent.pointerEnter(button('VIP'));
      expect(onFocus).toHaveBeenLastCalledWith('vip');
      fireEvent.pointerLeave(button('VIP'));
      expect(onFocus).toHaveBeenLastCalledWith(null);
      expect(onFocus).toHaveBeenCalledTimes(2);
    });

    it('does the same for keyboard focus and blur', () => {
      const onFocus = vi.fn();
      render(<VizLegend items={ITEMS} onToggle={() => undefined} onFocus={onFocus} />);
      act(() => button('Танцпол').focus());
      expect(onFocus).toHaveBeenLastCalledWith('dance');
      act(() => button('Танцпол').blur());
      expect(onFocus).toHaveBeenLastCalledWith(null);
    });

    it('does not highlight a hidden series on hover', () => {
      const onFocus = vi.fn();
      render(
        <VizLegend
          items={ITEMS}
          hidden={new Set<Id>(['vip'])}
          onToggle={() => undefined}
          onFocus={onFocus}
        />,
      );
      fireEvent.pointerEnter(button('VIP'));
      expect(onFocus).not.toHaveBeenCalled();
      fireEvent.pointerLeave(button('VIP'));
      expect(onFocus).toHaveBeenCalledWith(null);
    });
  });
});
