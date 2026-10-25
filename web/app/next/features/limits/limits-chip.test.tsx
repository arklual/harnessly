// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import LimitsChip from '@/next/features/limits/limits-chip';
import type { LimitsView, ProviderLimits } from '@/next/features/limits/model';

afterEach(cleanup);

const inHours = (h: number) => Date.now() / 1000 + h * 3600;

const limits = (over: Partial<ProviderLimits> = {}): ProviderLimits => ({
  fiveHour: {
    usedPct: 6,
    resetsAt: inHours(2),
    forecast: {
      status: 'lasts_until_reset',
      exhaustAt: null,
      exhaustRange: null,
      usedAtReset: 30,
      confidence: 'high',
      reason: null,
      lastAt: null,
      resetsAt: inHours(2),
      profile: false,
    },
  },
  sevenDay: { usedPct: 81, resetsAt: inHours(100), forecast: null },
  updatedAt: Date.now(),
  plan: 'Claude Max 20x',
  error: null,
  ...over,
});

const ready = (value: ProviderLimits | null, provider: 'claude' | 'codex' = 'claude'): LimitsView => ({
  state: 'ready',
  provider,
  limits: value,
});

describe('LimitsChip', () => {
  it('shows what is left of both windows beside the model, at once', () => {
    render(<LimitsChip view={ready(limits())} />);
    const chip = screen.getByRole('button', { name: /Claude limits/ });
    expect(chip.textContent).toContain('5h94%');
    expect(chip.textContent).toContain('wk19%');
  });

  it('opens a card with both windows, their resets and the forecast, and closes it with an exit', () => {
    render(<LimitsChip view={ready(limits())} />);
    expect(screen.queryByRole('dialog')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Claude limits/ }));
    const card = screen.getByRole('dialog');
    expect(card.textContent).toContain('94% left');
    expect(card.textContent).toContain('19% left');
    expect(card.textContent).toContain('Resets in 2 h');
    expect(card.textContent).toContain('Will last until the reset');
    expect(card.textContent).toContain('Claude Max 20x');
    expect(screen.getAllByRole('meter')).toHaveLength(2);

    fireEvent.keyDown(window, { key: 'Escape' });
    // The card leaves in its closing phase before it is gone.
    expect(screen.getByRole('dialog').getAttribute('data-state')).toBe('closing');
  });

  it('opens the card on hover after a short delay, keeps it on the card, and lets it go when the pointer goes', () => {
    vi.useFakeTimers();
    try {
      render(<LimitsChip view={ready(limits())} />);
      const chip = screen.getByRole('button', { name: /Claude limits/ });
      fireEvent.pointerOver(chip, { pointerType: 'mouse' });
      expect(screen.queryByRole('dialog')).toBeNull();
      act(() => void vi.advanceTimersByTime(120));
      const card = screen.getByRole('dialog');
      // From the chip onto the card: it stays.
      fireEvent.pointerOut(chip, { pointerType: 'mouse', relatedTarget: card });
      fireEvent.pointerOver(card, { pointerType: 'mouse', relatedTarget: chip });
      act(() => void vi.advanceTimersByTime(2000));
      expect(screen.getByRole('dialog').getAttribute('data-state')).toBe('open');
      // Off the card, resting away from both: it leaves.
      fireEvent.pointerOut(card, { pointerType: 'mouse', relatedTarget: document.body });
      act(() => void vi.advanceTimersByTime(400));
      expect(screen.getByRole('dialog').getAttribute('data-state')).toBe('closing');
    } finally {
      vi.useRealTimers();
    }
  });

  it('says so, and draws no number, for an agent that reports no window', () => {
    render(<LimitsChip view={ready(null, 'codex')} />);
    const chip = screen.getByRole('button', { name: /Codex limits: no data/ });
    expect(chip.textContent).toContain('—');
    fireEvent.click(chip);
    expect(screen.getByRole('dialog').textContent).toContain('Codex does not report its limits');
    expect(screen.queryAllByRole('meter')).toHaveLength(0);
  });

  it('tells a machine that did not answer, and shows the one window an agent has', () => {
    const { rerender } = render(<LimitsChip view={{ state: 'failed', provider: 'claude' }} />);
    act(() => fireEvent.click(screen.getByRole('button', { name: /Claude limits/ })));
    expect(screen.getByRole('dialog').textContent).toContain("Could not get the machine's limits");
    rerender(<LimitsChip view={ready(limits({ sevenDay: null }))} />);
    expect(screen.getAllByRole('meter')).toHaveLength(1);
  });
});
