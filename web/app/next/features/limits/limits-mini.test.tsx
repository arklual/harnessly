// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import LimitsMini from '@/next/features/limits/limits-mini';
import type { LimitsView, LimitWindow, ProviderLimits } from '@/next/features/limits/model';

const now = Math.floor(Date.now() / 1000);
const win = (usedPct: number, resetsIn: number): LimitWindow => ({
  usedPct,
  resetsAt: now + resetsIn,
  forecast: null,
});
const limits = (over: Partial<ProviderLimits>): ProviderLimits => ({
  fiveHour: null,
  sevenDay: null,
  month: null,
  balance: null,
  estimate: false,
  unofficial: false,
  updatedAt: null,
  plan: null,
  error: null,
  rejected: false,
  ...over,
});
const view = (l: ProviderLimits | null, provider: 'claude' | 'codex' = 'claude'): LimitsView => ({
  state: 'ready',
  provider,
  limits: l,
  spend: null,
});

describe('the limits card in the field (HRN-1552)', () => {
  it('tells every window with what is left, a bar and its reset, the least left as the big figure', () => {
    render(
      <LimitsMini
        view={view(
          limits({
            fiveHour: win(36, 2 * 3600 + 10 * 60),
            sevenDay: win(87, 3 * 86400),
            extra: [{ id: 'm', title: 'Fable', window: win(20, 3 * 86400) }],
          }),
        )}
        model="claude-fable-5-2"
      />,
    );
    const card = screen.getByRole('dialog', { hidden: true });
    expect(card.querySelector('.hx-ctx__pct')?.textContent).toBe('13%');
    expect(card.textContent).toContain('5 hours');
    expect(card.textContent).toContain('Fable · week');
    expect(card.querySelectorAll('.hx-ctx__win')).toHaveLength(3);
    expect(card.querySelector('.hx-ctx__win')?.textContent).toMatch(/2 h 10 min/);
  });

  it('says there is nothing when the machine reports nothing, and draws nothing with no reading at all', () => {
    const { container, rerender } = render(<LimitsMini view={view(limits({ error: 'denied' }), 'codex')} />);
    expect(container.querySelector('[role="dialog"]')?.textContent).toContain('denied');
    rerender(<LimitsMini view={view(null)} />);
    expect(container.querySelector('.hx-ctx')).toBeNull();
  });
});
