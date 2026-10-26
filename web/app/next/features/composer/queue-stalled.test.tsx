// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Queue from '@/next/features/composer/queue';
import type { QueueRow } from '@/next/features/composer/types';

afterEach(cleanup);

const show = (rows: QueueRow[], onRetry = vi.fn(() => Promise.resolve())) =>
  render(
    <Queue
      rows={rows}
      paused={null}
      waitingLimit={false}
      onEdit={vi.fn()}
      onRemove={vi.fn()}
      onResume={vi.fn()}
      onRetry={onRetry}
    />,
  );

describe('Queue row of a message the agent did not take', () => {
  it('a stalled message says it did not reach the agent and offers Retry and the bin, never a spinner', () => {
    const onRetry = vi.fn(() => Promise.resolve());
    show(
      [
        {
          clientId: 'c1',
          text: 'как дела',
          attachments: [],
          state: { k: 'queued', position: 1, reason: 'stalled' },
        },
      ],
      onRetry,
    );
    expect(screen.getByText("didn't go through")).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Remove from the queue' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(document.querySelector('.hx-queue__row')?.getAttribute('data-st')).toBe('failed');
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }));
    expect(onRetry).toHaveBeenCalledWith('c1');
  });

  it('a message the runtime did not know is back in the queue the same way', () => {
    show([
      {
        clientId: 'c2',
        text: 'И глянь логи',
        attachments: [],
        state: { k: 'failed', code: 'unconfirmed', retryable: true, at: 1 },
      },
    ]);
    expect(screen.getByText("didn't go through")).toBeTruthy();
    expect(screen.getByRole('button', { name: /Retry/ })).toBeTruthy();
  });
});
