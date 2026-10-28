// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import GoalNote from '@/next/features/chat/goal-note';

afterEach(cleanup);

describe('the achieved goal in history', () => {
  it('continues the goal once while the command is pending', async () => {
    let finish: () => void = () => {};
    const onContinue = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    render(<GoalNote text="Ship it" onContinue={onContinue} />);
    const button = screen.getByRole('button', { name: 'Continue' });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(onContinue).toHaveBeenCalledTimes(1);
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await act(async () => finish());
    expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('shows a failed continuation and retries it', async () => {
    const onContinue = vi
      .fn<() => Promise<void>>()
      .mockRejectedValueOnce(new Error('Runtime unavailable'))
      .mockResolvedValueOnce();
    render(<GoalNote text="Ship it" onContinue={onContinue} />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    });
    expect(screen.getByRole('alert').textContent).toBe('Runtime unavailable');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    });
    expect(onContinue).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
