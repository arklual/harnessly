// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import type { LoginFlow } from '@/next/protocol/v2';

const api = vi.hoisted(() => ({
  startLogin: vi.fn(),
  loadLogin: vi.fn(),
  sendLoginCode: vi.fn(),
  cancelLogin: vi.fn(),
  logoutAgent: vi.fn(),
  putAgentKey: vi.fn(),
}));
vi.mock('@/next/features/accounts/api', () => api);

import { useAgentCredentials } from '@/next/bindings/settings-credentials';

beforeAll(() => pinLocale('en'));
beforeEach(() => vi.resetAllMocks());

const flow = (over: Partial<LoginFlow>): LoginFlow => ({
  id: 'f1',
  provider: 'claude',
  mode: 'subscription',
  state: 'awaiting',
  input: 'code',
  url: 'https://example.test/auth',
  userCode: null,
  startedAt: 1,
  expiresAt: 2,
  reason: null,
  ...over,
});

describe('the sign-in of an agent', () => {
  it('waits for the code and sends it with the flow id', async () => {
    api.startLogin.mockResolvedValue({ ok: true, value: { flow: flow({}) } });
    api.sendLoginCode.mockResolvedValue({ ok: true, value: { flow: flow({ state: 'checking' }) } });
    const reload = vi.fn();
    const { result } = renderHook(() => useAgentCredentials('m', 'claude', null, reload));
    await act(async () => {
      await result.current.onLogin('subscription');
    });
    expect(result.current.login).toEqual({
      k: 'waiting',
      url: 'https://example.test/auth',
      userCode: null,
      asksCode: true,
    });
    await act(async () => {
      expect(await result.current.onSubmitCode('abc-123')).toBeNull();
    });
    expect(api.sendLoginCode).toHaveBeenCalledWith('m', 'claude', 'f1', 'abc-123');
    await waitFor(() => expect(result.current.login.k).toBe('checking'));
  });

  it('reads the settings again when the login is done', async () => {
    api.startLogin.mockResolvedValue({ ok: true, value: { flow: flow({ state: 'done' }) } });
    const reload = vi.fn();
    const { result } = renderHook(() => useAgentCredentials('m', 'claude', null, reload));
    await act(async () => {
      await result.current.onLogin('console');
    });
    expect(reload).toHaveBeenCalled();
    expect(result.current.login.k).toBe('idle');
  });

  it('says why a login was refused', async () => {
    api.startLogin.mockResolvedValue({ r: 'refused', code: 'conflict', status: 409 });
    const { result } = renderHook(() => useAgentCredentials('m', 'claude', null, vi.fn()));
    let text: string | null = null;
    await act(async () => {
      text = await result.current.onLogin('subscription');
    });
    expect(text).toBe('A sign-in is already running');
    expect(result.current.login.k).toBe('idle');
  });
});
