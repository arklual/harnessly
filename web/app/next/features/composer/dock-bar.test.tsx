// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import DockBar from '@/next/features/composer/dock-bar';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

describe('the notice of a removed key', () => {
  it('says which key is gone and offers the page and the handoff', async () => {
    const onOpen = vi.fn();
    const onContinue = vi.fn();
    render(<DockBar notice={{ kind: 'key', vendor: 'Kimi', onOpen, onContinue }} />);
    expect(screen.getByText(/The Kimi key was removed/)).toBeTruthy();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Models and keys' }));
    });
    expect(onOpen).toHaveBeenCalled();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Continue in another model' }));
    });
    expect(onContinue).toHaveBeenCalled();
  });

  it('offers no handoff where the machine cannot hand off', () => {
    render(<DockBar notice={{ kind: 'key', vendor: 'Kimi', onOpen: vi.fn(), onContinue: null }} />);
    expect(screen.queryByRole('button', { name: 'Continue in another model' })).toBeNull();
  });
});

describe('the notice of a recording that was not recognized (HRN-674)', () => {
  const base = { kind: 'voice' as const, ms: 159_000, more: 0, waiting: false, onDrop: async () => {} };

  it('without a link says it waits for the machine and offers only Delete', () => {
    render(<DockBar notice={{ ...base, waiting: true, busy: false, failure: null, onRetry: vi.fn() }} />);
    expect(screen.getByText('Recording 2:39 is kept')).toBeTruthy();
    expect(screen.getByText('I will recognize it when the machine is back')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Recognize' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Delete' }).hasAttribute('disabled')).toBe(false);
  });

  it('starts the recognition on the press', async () => {
    const onRetry = vi.fn();
    render(<DockBar notice={{ ...base, busy: false, failure: null, onRetry }} />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Recognize' }));
    });
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('is busy while the machine recognizes, and holds both actions', () => {
    render(<DockBar notice={{ ...base, busy: true, failure: null, onRetry: vi.fn() }} />);
    const retry = screen.getByRole('button', { name: 'Recognize' });
    expect(retry.getAttribute('data-busy')).toBe('true');
    expect(retry.hasAttribute('disabled')).toBe(true);
    expect(screen.getByRole('button', { name: 'Delete' }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Recognizing…')).toBeTruthy();
  });

  it('says why the last try did not work', () => {
    render(
      <DockBar
        notice={{ ...base, busy: false, failure: 'The machine did not answer in time', onRetry: vi.fn() }}
      />,
    );
    expect(screen.getByText('The machine did not answer in time')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Recognize' }).hasAttribute('disabled')).toBe(false);
  });
});
