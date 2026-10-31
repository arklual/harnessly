// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, expect, it, vi } from 'vitest';
import TerminalFailureCard, { retryText } from '@/next/features/chat/terminal-failure';
import type { FeedItem } from '@/next/features/chat/types';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

it('shows the terminal state and only opens target selection after the explicit action', () => {
  const choose = vi.fn();
  render(
    <TerminalFailureCard
      failure={{
        kind: 'quota_exhausted',
        at: 1,
        detail: 'Quota exhausted for this account',
        terminal: true,
        needsTarget: true,
      }}
      onChooseTarget={choose}
      onRetry={vi.fn()}
    />,
  );

  expect(screen.getByRole('alert').textContent).toContain('Model limit reached');
  expect(screen.getByRole('alert').textContent).toContain('Quota exhausted for this account');
  expect(choose).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Choose another model' }));
  expect(choose).toHaveBeenCalledTimes(1);
});

it('retries on the current model only after the explicit action', () => {
  const retry = vi.fn();
  render(
    <TerminalFailureCard
      failure={{
        kind: 'quota_exhausted',
        at: 1,
        detail: 'Quota exhausted for this account',
        terminal: true,
        needsTarget: true,
      }}
      onChooseTarget={null}
      onRetry={retry}
    />,
  );

  expect(retry).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  expect(retry).toHaveBeenCalledTimes(1);
});

it('keeps the target action visible but unavailable when the existing picker has no target', () => {
  render(
    <TerminalFailureCard
      failure={{
        kind: 'quota_exhausted',
        at: 1,
        detail: '',
        terminal: true,
        needsTarget: true,
      }}
      onChooseTarget={null}
      onRetry={vi.fn()}
    />,
  );
  expect(screen.getByRole('button', { name: 'Choose another model' }).hasAttribute('disabled')).toBe(true);
});

it('retries the stopped turn with its own message, and leaves a waiting queue to resume', () => {
  const turn = (text: string) => ({ kind: 'turn', key: text, prompt: { text } }) as unknown as FeedItem;
  expect(retryText([turn('first'), turn('build it')], 0)).toBe('build it');
  expect(retryText([turn('build it')], 2)).toBeNull();
  expect(retryText([], 0)).toBeNull();
  expect(retryText(null, 0)).toBeNull();
});
