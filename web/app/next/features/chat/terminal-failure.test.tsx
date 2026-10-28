// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, expect, it, vi } from 'vitest';
import TerminalFailureCard from '@/next/features/chat/terminal-failure';
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
    />,
  );

  expect(screen.getByRole('alert').textContent).toContain('Provider quota exhausted');
  expect(screen.getByRole('alert').textContent).toContain('Quota exhausted for this account');
  expect(choose).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Choose another model' }));
  expect(choose).toHaveBeenCalledTimes(1);
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
    />,
  );
  expect(screen.getByRole('button', { name: 'Choose another model' }).hasAttribute('disabled')).toBe(true);
});
