// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useRef, type ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HOVER } from '@/next/ui/hover-intent';
import { PortalContext } from '@/next/ui/portal';
import Tooltip from '@/next/ui/tooltip';

const Host = ({ children }: { children: ReactNode }) => {
  const ref = useRef<HTMLDivElement>(null);
  return (
    <PortalContext value={ref}>
      {children}
      <div ref={ref} />
    </PortalContext>
  );
};

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe('Tooltip', () => {
  it('waits its delay, explains the control, and goes at once when the pointer leaves', () => {
    const onClick = vi.fn();
    render(
      <Host>
        <Tooltip label="Send">
          <button type="button" onClick={onClick}>
            go
          </button>
        </Tooltip>
      </Host>,
    );
    const button = screen.getByRole('button');
    fireEvent.pointerOver(button, { pointerType: 'mouse' });
    act(() => void vi.advanceTimersByTime(HOVER.tipDelay - 1));
    expect(screen.queryByRole('tooltip')).toBeNull();
    act(() => void vi.advanceTimersByTime(1));
    const tip = screen.getByRole('tooltip');
    expect(tip.textContent).toBe('Send');
    expect(button.getAttribute('aria-describedby')).toBe(tip.id);

    // The child's own handler still runs; the click closes the hint.
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledOnce();
    expect(screen.getByRole('tooltip').getAttribute('data-state')).toBe('closing');
  });

  it('opens the next hint of a row at once', () => {
    render(
      <Host>
        <Tooltip label="One">
          <button type="button">1</button>
        </Tooltip>
        <Tooltip label="Two">
          <button type="button">2</button>
        </Tooltip>
      </Host>,
    );
    const [one, two] = screen.getAllByRole('button');
    fireEvent.pointerOver(one, { pointerType: 'mouse' });
    act(() => void vi.advanceTimersByTime(HOVER.tipDelay));
    fireEvent.pointerOut(one, { pointerType: 'mouse', relatedTarget: two });
    fireEvent.pointerOver(two, { pointerType: 'mouse', relatedTarget: one });
    const open = screen.getAllByRole('tooltip').filter((el) => el.getAttribute('data-state') === 'open');
    expect(open.map((el) => el.textContent)).toEqual(['Two']);
  });

  it('never shows on touch', () => {
    render(
      <Host>
        <Tooltip label="Send">
          <button type="button">go</button>
        </Tooltip>
      </Host>,
    );
    fireEvent.pointerOver(screen.getByRole('button'), { pointerType: 'touch' });
    act(() => void vi.advanceTimersByTime(HOVER.tipDelay * 2));
    expect(screen.queryByRole('tooltip')).toBeNull();
  });
});
