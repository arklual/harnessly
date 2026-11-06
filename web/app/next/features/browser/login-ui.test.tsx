// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BrowserLoginAskHost } from '@/next/bindings/browser-login-asks';
import { LoginBanner } from './login-banner';
import { answerLogin, loadLogins, type ILoginAsk } from './login-api';
import { chatPaneOf, resetChatPanes } from '@/next/stores/pane-state';
import type { MachineId } from '@/next/protocol/v2';
vi.mock('@/next/i18n', () => ({ t: (key: string) => key.split('.').at(-1) }));
vi.mock('./login-api', () => ({ loadLogins: vi.fn(), answerLogin: vi.fn() }));
vi.mock('@/next/features/shell/modal', () => ({
  default: ({ children, title }: { children: React.ReactNode; title: string }) => (
    <div role="dialog" aria-label={title}>
      {children}
    </div>
  ),
}));
const machine = 'local' as MachineId;
const ask: ILoginAsk = {
  id: 'request',
  conv: 'task',
  title: 'Login task',
  why: 'Please sign in to continue',
  url: 'https://example.org/login',
  tab: 'login-tab',
  at: 1,
};
beforeEach(() => {
  localStorage.clear();
  resetChatPanes();
  vi.mocked(loadLogins).mockResolvedValue([ask]);
  vi.mocked(answerLogin).mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  resetChatPanes();
});
describe('manual browser login', () => {
  it('shows a global card outside the requesting chat, then opens its browser panel', async () => {
    const onOpen = vi.fn();
    render(<BrowserLoginAskHost machine={machine} conv="other" onOpen={onOpen} />);
    await screen.findByRole('dialog', { name: 'loginTitle' });
    expect(screen.getByText(ask.why)).toBeTruthy();
    expect(chatPaneOf('other').open).toBe(false);
    fireEvent.click(screen.getByText('loginOpen'));
    expect(onOpen).toHaveBeenCalledWith('task');
    expect(chatPaneOf('task')).toMatchObject({
      open: true,
      active: 'agent-browser-login',
      view: { 'browser-login-tab': 'login-tab' },
    });
  });
  it('auto-opens once in the requesting chat and leaves a later manual close alone', async () => {
    const view = render(<BrowserLoginAskHost machine={machine} conv="task" onOpen={vi.fn()} />);
    await waitFor(() => expect(chatPaneOf('task').open).toBe(true));
    expect(screen.queryByRole('dialog')).toBeNull();
    const { chatPanes } = await import('@/next/stores/pane-state');
    act(() => chatPanes.update('task', machine, (pane) => ({ ...pane, open: false })));
    view.rerender(<BrowserLoginAskHost machine={machine} conv="task" onOpen={vi.fn()} />);
    expect(chatPaneOf('task').open).toBe(false);
  });
  it('allows cancellation from another chat without switching', async () => {
    const onOpen = vi.fn();
    render(<BrowserLoginAskHost machine={machine} conv="other" onOpen={onOpen} />);
    await screen.findByRole('dialog');
    fireEvent.click(screen.getByText('cancel'));
    await waitFor(() => expect(answerLogin).toHaveBeenCalledWith(machine, 'request', 'cancel'));
    expect(onOpen).not.toHaveBeenCalled();
  });
  it('keeps the banner and displays a failed decision, then retries Done or Cancel', async () => {
    vi.mocked(answerLogin).mockRejectedValueOnce(new Error('Page disconnected'));
    render(<LoginBanner machine={machine} ask={ask} />);
    expect(screen.getByText(ask.why)).toBeTruthy();
    fireEvent.click(screen.getByText('loginDone'));
    await screen.findByRole('alert');
    expect(screen.getByRole('alert').textContent).toBe('Page disconnected');
    fireEvent.click(screen.getByText('cancel'));
    await waitFor(() => expect(answerLogin).toHaveBeenLastCalledWith(machine, 'request', 'cancel'));
  });
});
