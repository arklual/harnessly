// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { LoginState } from '@/next/features/accounts/model';
import LoginPanel from '@/next/features/accounts/login-panel';
import { pinLocale } from '@/next/i18n';
import type { AgentAccount } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const OFF: AgentAccount = { signedIn: false, method: null, plan: null, who: null };
const ON: AgentAccount = { signedIn: true, method: 'subscription', plan: 'Max', who: null };

const setup = (login: LoginState, account = OFF, via: 'subscription' | 'console' = 'subscription') => {
  const onLogin = vi.fn(() => Promise.resolve(null as string | null));
  const onSubmitCode = vi.fn(() => Promise.resolve(null as string | null));
  const onCancel = vi.fn(() => Promise.resolve());
  const onLogout = vi.fn(() => Promise.resolve(null as string | null));
  render(
    <LoginPanel
      via={via}
      account={account}
      login={login}
      onLogin={onLogin}
      onSubmitCode={onSubmitCode}
      onCancel={onCancel}
      onLogout={onLogout}
    />,
  );
  return { onLogin, onSubmitCode, onCancel, onLogout };
};

describe('the in-page sign-in', () => {
  it('starts the login on the machine', async () => {
    const { onLogin } = setup({ k: 'idle' });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Sign in with a subscription' }));
    });
    expect(onLogin).toHaveBeenCalledWith('subscription');
  });

  it('starts the console login for API billing', async () => {
    const { onLogin } = setup({ k: 'idle' }, OFF, 'console');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Sign in for API billing' }));
    });
    expect(onLogin).toHaveBeenCalledWith('console');
  });

  it('says the machine checks the code', () => {
    setup({ k: 'checking' });
    expect(screen.getByText('Checking the code on the machine…')).toBeTruthy();
  });

  it('shows the link in a new tab without leaking the opener, and the code to type', () => {
    setup({ k: 'waiting', url: 'https://example.test/device', userCode: 'WDJB-MJHT', asksCode: false });
    const link = screen.getByRole('link', { name: 'Open the sign-in page' });
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(screen.getByText('WDJB-MJHT')).toBeTruthy();
  });

  it('takes the code back in a masked field and empties it as it is sent', async () => {
    const { onSubmitCode } = setup({
      k: 'waiting',
      url: 'https://example.test/o',
      userCode: null,
      asksCode: true,
    });
    const field = screen.getByLabelText<HTMLInputElement>('Code from the sign-in page');
    expect(field.type).toBe('password');
    expect(field.autocomplete).toBe('off');
    fireEvent.change(field, { target: { value: ' abc-123 ' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    });
    expect(onSubmitCode).toHaveBeenCalledWith('abc-123');
    expect(field.value).toBe('');
  });

  it('signs out only on the second press', async () => {
    const { onLogout } = setup({ k: 'idle' }, ON);
    fireEvent.click(screen.getByRole('button', { name: 'Sign out' }));
    expect(onLogout).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.click(
        screen.getByRole('button', { name: 'Sign out for sure? Running chats will lose the login' }),
      );
    });
    expect(onLogout).toHaveBeenCalled();
  });

  it('says why the login failed and offers another try', () => {
    setup({ k: 'failed', reason: 'Timed out' });
    expect(screen.getByText('Timed out')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });
});
