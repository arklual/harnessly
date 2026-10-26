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
