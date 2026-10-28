// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { NotifyBotsPage } from '@/next/bindings/settings-notify-bots';
import { setFetch } from '@/next/core/net/http';
import { readShared, type ISharedState } from '@/next/features/notify-bots/api';
import NotifySharedSection, {
  type INotifySharedActions,
} from '@/next/features/settings/notify-shared-section';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  setFetch((url, init) => fetch(url, init));
});

const LINK = 'https://t.me/harnessly_bot?start=c0de';

// The shared bot as GET <platform>/notify-shared answers it (cmd/runtime/routes_notifybots.go).
const wire = (over: Partial<ISharedState> = {}): ISharedState => ({
  enabled: true,
  bot: 'harnessly_bot',
  chat: null,
  instruction: '',
  binding: null,
  ...over,
});

const actions = (): INotifySharedActions & Record<string, ReturnType<typeof vi.fn>> => ({
  onLink: vi.fn(async () => ({ link: LINK })),
  onUnlink: vi.fn(async () => null),
  onSave: vi.fn(async () => null),
  onTest: vi.fn(async () => null),
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

describe('the shared bot reader', () => {
  it('takes only a t.me link', () => {
    expect(() => readShared(wire({ binding: { link: 'https://evil.test/x', expiresAt: 1 } }))).toThrow();
    expect(readShared(wire({ binding: { link: LINK, expiresAt: 1 } })).binding?.link).toBe(LINK);
  });
});

describe('Telegram through the shared bot', () => {
  it('opens the bot with the code in a tab opened by the press itself', async () => {
    const tab = { opener: {}, location: { href: '' }, close: vi.fn() };
    const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window);
    const a = actions();
    render(<NotifySharedSection state={wire()} {...a} />);
    await act(async () => {
      fireEvent.click(screen.getByText('Connect Telegram'));
    });
    expect(open).toHaveBeenCalledWith('', '_blank');
    expect(tab.opener).toBeNull();
    expect(tab.location.href).toBe(LINK);
  });

  it('closes the tab and says why when there is no link', async () => {
    const tab = { opener: {}, location: { href: '' }, close: vi.fn() };
    vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window);
    const a = actions();
    vi.mocked(a.onLink).mockResolvedValue({ error: 'The platform did not answer' });
    render(<NotifySharedSection state={wire()} {...a} />);
    await act(async () => {
      fireEvent.click(screen.getByText('Connect Telegram'));
    });
    expect(tab.close).toHaveBeenCalled();
    expect(screen.getByText('The platform did not answer')).toBeTruthy();
  });

  it('waits for Start with a link to open the bot again', () => {
    render(<NotifySharedSection state={wire({ binding: { link: LINK, expiresAt: 2 } })} {...actions()} />);
    expect(screen.getByText(/Press Start/)).toBeTruthy();
    expect((screen.getByText('Open it again').closest('a') as HTMLAnchorElement).href).toBe(LINK);
  });

  it('shows the connected chat, tests it and disconnects only on the second press', async () => {
    const a = actions();
    render(<NotifySharedSection state={wire({ chat: { title: 'Sergey' } })} {...a} />);
    expect(screen.getByText('Connected: Sergey')).toBeTruthy();
    await act(async () => {
      fireEvent.click(screen.getByText('Send a test'));
    });
    expect(a.onTest).toHaveBeenCalled();
    fireEvent.click(screen.getByText('Disconnect'));
    expect(a.onUnlink).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.click(screen.getByText('Disconnect?'));
    });
    expect(a.onUnlink).toHaveBeenCalled();
  });

  it('saves the instruction trimmed', async () => {
    const a = actions();
    render(<NotifySharedSection state={wire({ instruction: 'old' })} {...a} />);
    fireEvent.change(screen.getByLabelText('When to use it'), { target: { value: '  when it burns  ' } });
    await act(async () => {
      fireEvent.click(screen.getByText('Save'));
    });
    expect(a.onSave).toHaveBeenCalledWith('when it burns');
  });

  it('shows the shared bot, not the own bots, when the machine has one', async () => {
    const urls: string[] = [];
    setFetch(async (url) => {
      urls.push(url);
      if (url.includes('/notify-shared'))
        return json(200, { shared: wire({ chat: { title: 'Sergey' } }), ownBots: false, problem: '' });
      return json(200, { bots: [] });
    });
    render(<NotifyBotsPage machine="local" />);
    await waitFor(() => expect(screen.getByText('Connected: Sergey')).toBeTruthy());
    expect(screen.queryByText('Add a bot')).toBeNull();
    expect(urls.some((u) => u.includes('/notify-bots'))).toBe(false);
  });

  it('falls back to the own bots when there is no shared bot', async () => {
    setFetch(async (url) => {
      if (url.includes('/notify-shared'))
        return json(200, { shared: wire({ enabled: false, bot: '' }), ownBots: true, problem: '' });
      return json(200, { bots: [] });
    });
    render(<NotifyBotsPage machine="local" />);
    await waitFor(() => expect(screen.getByText('Add a bot')).toBeTruthy());
    expect(screen.queryByText('Connect Telegram')).toBeNull();
  });
});
