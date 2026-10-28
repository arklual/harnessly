// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { NotifyBotsPage } from '@/next/bindings/settings-notify-bots';
import { setFetch } from '@/next/core/net/http';
import { readBot, type INotifyBot } from '@/next/features/notify-bots/api';
import NotifyBotsSection, { type INotifyBotsActions } from '@/next/features/settings/notify-bots-section';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));
afterEach(() => {
  cleanup();
  setFetch((url, init) => fetch(url, init));
});

const TOKEN = '123456789:AAH-abcdefghijklmnopqrstuvwxyz0123456';

// A bot as GET <platform>/notify-bots answers it (cmd/runtime/routes_notifybots.go).
const wire = (over: Record<string, unknown> = {}) => ({
  id: 'nb_1',
  name: 'Alerts',
  username: 'ping_bot',
  title: 'Ping',
  tokenSet: true,
  tokenTail: '3456',
  chat: { title: 'Sergey', type: 'private' },
  instruction: 'When a long task is done\nor you need my answer',
  binding: null,
  createdAt: 1,
  updatedAt: 1,
  ...over,
});

const actions = (): INotifyBotsActions & Record<string, ReturnType<typeof vi.fn>> => ({
  onAdd: vi.fn(async () => null),
  onSave: vi.fn(async () => null),
  onToken: vi.fn(async () => null),
  onBind: vi.fn(async () => null),
  onStopBind: vi.fn(async () => null),
  onTest: vi.fn(async () => null),
  onRemove: vi.fn(async () => null),
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

describe('the bot reader', () => {
  it('keeps nothing of a token, even one sent by mistake', () => {
    const bot = readBot(wire({ token: TOKEN }));
    expect(JSON.stringify(bot)).not.toContain('abcdefghij');
    expect(bot.tokenTail).toBe('3456');
  });
  it('refuses a bot without its fields', () => {
    expect(() => readBot({ id: 'x' })).toThrow();
  });
});

describe('Telegram bots in Settings', () => {
  it('lists a bot by its name, chat and the first line of its instruction', () => {
    render(<NotifyBotsSection bots={[readBot(wire())]} open={null} onOpen={() => {}} {...actions()} />);
    expect(screen.getByText('Alerts')).toBeTruthy();
    expect(screen.getByText('@ping_bot → Sergey')).toBeTruthy();
    expect(screen.getByText('When a long task is done')).toBeTruthy();
  });

  it('shows the loading line while the list is on its way', () => {
    render(<NotifyBotsSection bots={null} open={null} onOpen={() => {}} {...actions()} />);
    expect(screen.queryByText('Add a bot')).toBeNull();
  });

  it('removes a bot only on the second press', async () => {
    const a = actions();
    render(<NotifyBotsSection bots={[readBot(wire())]} open="nb_1" onOpen={() => {}} {...a} />);
    fireEvent.click(screen.getByText('Remove'));
    expect(a.onRemove).not.toHaveBeenCalled();
    await act(async () => {
      fireEvent.click(screen.getByText('Remove for sure?'));
    });
    expect(a.onRemove).toHaveBeenCalledWith('nb_1');
  });

  it('saves the name and the instruction as typed, trimmed', async () => {
    const a = actions();
    render(<NotifyBotsSection bots={[readBot(wire())]} open="nb_1" onOpen={() => {}} {...a} />);
    const save = screen.getByText('Save').closest('button') as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('When to use it'), {
      target: { value: '  Only when it burns  ' },
    });
    await act(async () => {
      fireEvent.click(save);
    });
    expect(a.onSave).toHaveBeenCalledWith('nb_1', { name: 'Alerts', instruction: 'Only when it burns' });
  });

  it('shows the link and the group command while a chat is being bound', () => {
    const bot = readBot(
      wire({
        chat: null,
        binding: {
          state: 'waiting',
          code: 'c0de',
          link: 'https://t.me/ping_bot?start=c0de',
          command: '/start@ping_bot c0de',
          expiresAt: 2,
          reason: '',
        },
      }),
    );
    render(<NotifyBotsSection bots={[bot]} open="nb_1" onOpen={() => {}} {...actions()} />);
    const link = screen.getByText('Open the bot and press Start').closest('a') as HTMLAnchorElement;
    expect(link.href).toBe('https://t.me/ping_bot?start=c0de');
    expect(screen.getByText('/start@ping_bot c0de')).toBeTruthy();
    expect(screen.getByText('@ping_bot · no chat yet')).toBeTruthy();
  });

  it('links a bot through the runtime, starts its binding and never shows the token', async () => {
    const calls: { method: string; url: string; body: unknown }[] = [];
    let bots: INotifyBot[] | Record<string, unknown>[] = [];
    setFetch(async (url, init) => {
      const body = typeof init.body === 'string' ? JSON.parse(init.body) : null;
      const method = init.method ?? 'GET';
      calls.push({ method, url, body });
      if (url.includes('/notify-shared'))
        return json(200, {
          shared: { enabled: false, bot: '', chat: null, instruction: '', binding: null },
          ownBots: true,
          problem: '',
        });
      if (method === 'GET') return json(200, { bots });
      if (method === 'POST' && url.includes('/notify-bots?')) {
        if (body.token !== TOKEN) return json(400, { error: 'invalid-token' });
        bots = [wire({ chat: null })];
        return json(200, { bot: bots[0] });
      }
      if (method === 'POST' && url.includes('/bind')) {
        const bound = wire({
          chat: null,
          binding: {
            state: 'waiting',
            code: 'c0de',
            link: 'https://t.me/ping_bot?start=c0de',
            command: '/start@ping_bot c0de',
            expiresAt: 2,
            reason: '',
          },
        });
        return json(200, { bot: bound });
      }
      return json(404, { error: 'not-found' });
    });
    const { container } = render(<NotifyBotsPage machine="local" />);
    await waitFor(() => expect(screen.getByText('Add a bot')).toBeTruthy());
    expect(screen.getByText(/Link a bot/)).toBeTruthy();

    fireEvent.click(screen.getByText('Add a bot'));
    const field = screen.getAllByLabelText('Bot token')[0] as HTMLInputElement;
    fireEvent.change(field, { target: { value: 'not-it:AAH-abcdefghijklmnopqrstuvwxyz0123456' } });
    await act(async () => {
      fireEvent.click(screen.getByText('Connect'));
    });
    await waitFor(() => expect(screen.getByText(/Telegram did not accept the token/)).toBeTruthy());
    expect(field.value).toBe('');

    fireEvent.change(field, { target: { value: TOKEN } });
    await act(async () => {
      fireEvent.click(screen.getByText('Connect'));
    });
    await waitFor(() => expect(screen.getByText('Open the bot and press Start')).toBeTruthy());
    const posts = calls.filter((c) => c.method === 'POST' && c.url.includes('/notify-bots?'));
    expect(posts.map((c) => c.body)).toEqual([
      { token: 'not-it:AAH-abcdefghijklmnopqrstuvwxyz0123456' },
      { token: TOKEN },
    ]);
    expect(calls.some((c) => c.method === 'POST' && c.url.includes('/notify-bots/nb_1/bind'))).toBe(true);
    // The token is only ever in the request: the page shows its last characters.
    expect(container.innerHTML).not.toContain('abcdefghij');
    expect(screen.getByText('@ping_bot · ••••3456')).toBeTruthy();
  });
});
