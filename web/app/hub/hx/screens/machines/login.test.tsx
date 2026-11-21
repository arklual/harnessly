// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { OrgApi } from '../../../org/api';
import type { AgentLogin } from '../../../org/api/machines';

const state = vi.hoisted(() => ({ api: {} as unknown as OrgApi }));

vi.mock('../../../org/context', () => ({ useOrg: () => ({ api: state.api }) }));

import { AgentSignIn, safeLoginUrl } from './login';

const login = (over: Partial<AgentLogin> = {}): AgentLogin => ({
  id: 'l1',
  provider: 'claude',
  state: 'url',
  url: 'https://claude.ai/oauth/authorize?x=1',
  needsCode: true,
  error: null,
  ...over,
});

const mount = (api: Partial<OrgApi>) => {
  state.api = api as OrgApi;
  const onPoll = vi.fn();
  render(<AgentSignIn machineId="m1" tenantId="t1" provider="claude" onPoll={onPoll} />);
  return onPoll;
};

afterEach(() => cleanup());

describe('safeLoginUrl', () => {
  it('lets through only the provider’s own https sign-in page', () => {
    expect(safeLoginUrl('claude', 'https://claude.ai/oauth?a=1')).toBe('https://claude.ai/oauth?a=1');
    expect(safeLoginUrl('codex', 'https://auth.openai.com/authorize')).toBe(
      'https://auth.openai.com/authorize',
    );
    expect(safeLoginUrl('claude', 'https://auth.openai.com/authorize')).toBeNull();
    expect(safeLoginUrl('claude', 'http://claude.ai/oauth')).toBeNull();
    expect(safeLoginUrl('claude', 'https://claude.ai:8443/oauth')).toBeNull();
    expect(safeLoginUrl('claude', 'https://user@claude.ai/oauth')).toBeNull();
    expect(safeLoginUrl('claude', 'https://claude.ai.evil.example/oauth')).toBeNull();
    expect(safeLoginUrl('claude', 'not a url')).toBeNull();
  });
});

describe('agent sign-in in the machine card', () => {
  it('starts a sign-in, shows the link and sends the code to the machine', async () => {
    const startAgentLogin = vi.fn(async () => login());
    const sendLoginCode = vi.fn(async () => undefined);
    const onPoll = mount({ startAgentLogin, sendLoginCode });
    fireEvent.click(screen.getByRole('button', { name: /Войти по ссылке/ }));
    const link = await screen.findByRole('link', { name: 'Открыть в браузере' });
    expect(link.getAttribute('href')).toBe('https://claude.ai/oauth/authorize?x=1');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(startAgentLogin).toHaveBeenCalledWith('m1', 'claude');
    fireEvent.change(screen.getByLabelText('Код со страницы входа'), { target: { value: ' abc#123 ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Отправить' }));
    await screen.findByText(/Код отправлен/);
    expect(sendLoginCode).toHaveBeenCalledWith('m1', 'l1', 'abc#123');
    expect(onPoll).toHaveBeenCalled();
  });

  it('does not show a link to another site', async () => {
    mount({ startAgentLogin: vi.fn(async () => login({ url: 'https://evil.example/login' })) });
    fireEvent.click(screen.getByRole('button', { name: /Войти по ссылке/ }));
    await screen.findByText(/не на страницу Anthropic/);
    expect(screen.queryByRole('link')).toBeNull();
  });

  it('says why the hub refused and lets cancel go back to the start', async () => {
    const cancelAgentLogin = vi.fn(async () => undefined);
    mount({
      startAgentLogin: vi
        .fn()
        .mockRejectedValueOnce(new Error('нет прав'))
        .mockResolvedValueOnce(login({ needsCode: false })),
      cancelAgentLogin,
    });
    fireEvent.click(screen.getByRole('button', { name: /Войти по ссылке/ }));
    await screen.findByRole('alert');
    fireEvent.click(screen.getByRole('button', { name: /Войти по ссылке/ }));
    await screen.findByText(/статус обновится сам/);
    fireEvent.click(screen.getByRole('button', { name: 'Отмена' }));
    await waitFor(() => expect(cancelAgentLogin).toHaveBeenCalledWith('m1', 'l1'));
    expect(screen.getByRole('button', { name: /Войти по ссылке/ })).toBeTruthy();
  });

  it('offers to try again after an expired link', async () => {
    mount({ startAgentLogin: vi.fn(async () => login({ state: 'expired' })) });
    fireEvent.click(screen.getByRole('button', { name: /Войти по ссылке/ }));
    await screen.findByText('Ссылка устарела.');
    expect(screen.getByRole('button', { name: /Ещё раз/ })).toBeTruthy();
  });
});
