// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { Config } from '../../lib/types';

const state = vi.hoisted(() => ({ flow: {} as Record<string, unknown>, peek: vi.fn(), me: null as unknown }));

vi.mock('../../screens/auth-flow', () => ({ useAuthFlow: () => state.flow }));
vi.mock('../../lib/env', () => ({ useApi: () => ({ invites: { peek: state.peek, accept: vi.fn() } }) }));
vi.mock('../../lib/session', () => ({ useSession: () => ({ me: state.me, reload: vi.fn() }) }));
vi.mock('@kit', () => ({ useTheme: () => ({ resolved: 'light', setMode: vi.fn() }) }));

import { HxAuth } from './signin';
import { HxInvite } from './invite';

const flow = (over: Record<string, unknown> = {}, config: Partial<Config> = {}) => {
  state.flow = {
    config: { signup: 'open', auth: { email: { mode: 'smtp' } }, ...config },
    login: true,
    phase: 'form',
    email: '',
    code: '',
    busy: null,
    error: null,
    codeBad: false,
    secondsLeft: 0,
    redirectError: null,
    oauth: [],
    passkey: false,
    switchHref: '/signup',
    setEmail: vi.fn(),
    send: vi.fn(),
    changeEmail: vi.fn(),
    onCode: vi.fn(),
    verify: vi.fn(),
    startOauth: vi.fn(),
    signInWithPasskey: vi.fn(),
    ...over,
  };
};

afterEach(() => {
  cleanup();
  state.me = null;
});

describe('HxAuth', () => {
  it('shows only e-mail when the instance offers nothing else', () => {
    flow();
    render(<HxAuth mode="signin" />);
    expect(screen.getByRole('button', { name: 'Получить код' })).toBeTruthy();
    expect(screen.queryByText('GitHub')).toBeNull();
  });

  it('shows the buttons /api/config lists', () => {
    flow({
      passkey: true,
      oauth: [
        { id: 'github', kind: 'github', name: 'GitHub' },
        { id: 'yandex', kind: 'yandex', name: 'Яндекс' },
        { id: 'oidc', kind: 'oidc', name: 'Example SSO' },
      ],
    });
    render(<HxAuth mode="signin" />);
    expect(screen.getByRole('button', { name: /GitHub/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Яндекс/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Example SSO/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /passkey|ключ/i })).toBeTruthy();
  });

  it('says plainly that sign-up is closed when there is no other way in', () => {
    flow({ login: false }, { signup: 'invite' });
    render(<HxAuth mode="signup" />);
    expect(screen.getByText('Регистрация закрыта')).toBeTruthy();
  });
});

describe('HxInvite', () => {
  it('tells an expired invitation apart and points on', async () => {
    state.peek.mockResolvedValue({ valid: false });
    render(<HxInvite token="t" />);
    await waitFor(() =>
      expect(screen.getAllByText('Приглашение больше не действует').length).toBeGreaterThan(0),
    );
  });
});
