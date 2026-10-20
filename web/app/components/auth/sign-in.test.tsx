// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const calls: string[] = [];
const fail = vi.hoisted(() => ({ login: false }));

vi.mock('@/runtime/endpoints', () => ({
  auth: {
    setup: async (password: string) => void calls.push(`setup:${password}`),
    login: async (password: string) => {
      calls.push(`login:${password}`);
      if (fail.login) throw new Error('Invalid password.');
    },
  },
}));
vi.mock('@/platform/config', () => ({ appConfig: () => ({ productName: 'harnessly' }) }));
vi.mock('@/components/agents/hl-mark', () => ({ default: () => null }));

import SignIn from './sign-in';

const type = (label: RegExp | string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } });

beforeEach(() => {
  calls.length = 0;
  fail.login = false;
});
afterEach(cleanup);

describe('SignIn', () => {
  it('signs in with the machine password', async () => {
    const done = vi.fn();
    render(<SignIn needsSetup={false} onSignedIn={done} />);
    expect(screen.getByTestId('signin-form')).toBeTruthy();
    type('Password', 'hunter22');
    fireEvent.click(screen.getByRole('button'));
    await waitFor(() => expect(done).toHaveBeenCalled());
    expect(calls).toEqual(['login:hunter22']);
  });

  it('sets the first password and signs in with it', async () => {
    const done = vi.fn();
    render(<SignIn needsSetup onSignedIn={done} />);
    expect(screen.getByTestId('setup-form')).toBeTruthy();
    type('Password', 'hunter22');
    type('Confirm password', 'hunter22');
    fireEvent.click(screen.getByRole('button'));
    await waitFor(() => expect(done).toHaveBeenCalled());
    expect(calls).toEqual(['setup:hunter22', 'login:hunter22']);
  });

  it('asks for the initial password first when the runtime holds the setup behind one', async () => {
    const done = vi.fn();
    render(<SignIn needsSetup requiresAuth onSignedIn={done} />);
    expect(screen.getByTestId('init-form')).toBeTruthy();
    expect(screen.queryByLabelText('Confirm password')).toBeNull();
    type('Password', 'initial-pass');
    fireEvent.click(screen.getByRole('button'));
    // The session the initial password gave is what lets the setup through: then the person's own password.
    await waitFor(() => expect(screen.getByTestId('setup-form')).toBeTruthy());
    expect(done).not.toHaveBeenCalled();
    type('Password', 'my-own-pass');
    type('Confirm password', 'my-own-pass');
    fireEvent.click(screen.getByRole('button'));
    await waitFor(() => expect(done).toHaveBeenCalled());
    expect(calls).toEqual(['login:initial-pass', 'setup:my-own-pass', 'login:my-own-pass']);
  });

  it('stays on the initial password when it is wrong', async () => {
    fail.login = true;
    const done = vi.fn();
    render(<SignIn needsSetup requiresAuth onSignedIn={done} />);
    type('Password', 'nope');
    fireEvent.click(screen.getByRole('button'));
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Invalid password.'));
    expect(screen.getByTestId('init-form')).toBeTruthy();
    expect(done).not.toHaveBeenCalled();
  });
});
