// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import GoalBar from '@/next/features/composer/goal-bar';

afterEach(cleanup);

describe('the goal bar', () => {
  it('shows the goal and stops it', async () => {
    const onStop = vi.fn(() => Promise.resolve());
    render(<GoalBar goal={{ text: 'Ship the contract', status: 'active', since: 1 }} onStop={onStop} />);
    expect(screen.getByRole('status').textContent).toContain('Goal: Ship the contract');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Stop' }));
    });
    expect(onStop).toHaveBeenCalledTimes(1);
  });

  it('says a goal is paused', () => {
    render(<GoalBar goal={{ text: 'Ship it', status: 'paused', since: 1 }} onStop={vi.fn()} />);
    expect(screen.getByRole('status').textContent).toContain('Goal paused: Ship it');
  });

  it('keeps a met goal as a line the person hides', () => {
    render(<GoalBar goal={{ text: 'Ship it', status: 'met', since: 1 }} onStop={vi.fn()} />);
    expect(screen.getByRole('status').textContent).toContain('Goal reached: Ship it');
    expect(screen.queryByRole('button', { name: 'Stop' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Hide' }));
    expect(screen.queryByRole('status')?.dataset.state ?? 'closing').toBe('closing');
  });

  it('draws nothing without a goal', () => {
    const { container } = render(<GoalBar goal={null} onStop={vi.fn()} />);
    expect(container.firstChild).toBeNull();
  });
});
