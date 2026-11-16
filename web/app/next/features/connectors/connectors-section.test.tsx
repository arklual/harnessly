// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { readConnectorConn, readSpec } from '@/next/features/connectors/api';
import ConnectorsSection, {
  type IConnectorsSectionProps,
} from '@/next/features/connectors/connectors-section';
import { pinLocale } from '@/next/i18n';

beforeAll(() => {
  pinLocale('en');
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

// The RSS connector's Spec as GET <platform>/connectors answers it (internal/connectors/rss), plus a mail one with a
// choice and a secret field.
const rss = readSpec({
  id: 'rss',
  kind: 'feed',
  title: { en: 'RSS or Atom feed', ru: 'Лента RSS или Atom' },
  summary: { en: 'New posts of a blog' },
  fields: [
    { id: 'url', type: 'url', label: { en: 'Feed address' }, placeholder: 'https://example.com/feed.xml' },
    { id: 'every', type: 'number', label: { en: 'Check every, minutes' }, optional: true, default: '30' },
  ],
});
const mail = readSpec({
  id: 'imap',
  kind: 'mail',
  title: { en: 'Mail (IMAP)' },
  fields: [
    {
      id: 'security',
      type: 'choice',
      label: { en: 'Security' },
      choices: [
        { value: 'tls', label: { en: 'TLS' } },
        { value: 'starttls', label: { en: 'STARTTLS' } },
      ],
    },
    { id: 'password', type: 'password', secret: true, label: { en: 'App password' } },
  ],
});

const props = (over: Partial<IConnectorsSectionProps> = {}): IConnectorsSectionProps => ({
  catalog: [mail, rss],
  connections: [],
  onConnect: vi.fn(async () => null),
  onCheck: vi.fn(async () => null),
  onRemove: vi.fn(async () => null),
  ...over,
});

describe('ConnectorsSection', () => {
  it('groups the catalog by kind and connects with the values of the form', async () => {
    const p = props();
    render(<ConnectorsSection {...p} />);
    expect(screen.getByText('Mail')).toBeTruthy();
    expect(screen.getByText('Feeds')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /RSS or Atom feed/ }));
    const url = screen.getByLabelText('Feed address');
    fireEvent.change(url, { target: { value: 'https://example.com/feed.xml' } });
    fireEvent.click(screen.getAllByRole('button', { name: 'Connect' })[1]);
    await waitFor(() =>
      expect(p.onConnect).toHaveBeenCalledWith({
        connector: 'rss',
        name: '',
        values: { url: 'https://example.com/feed.xml' },
      }),
    );
  });

  it('draws a choice as buttons, a secret as a password, and shows the refusal', async () => {
    const p = props({ onConnect: vi.fn(async () => 'Wrong password') });
    render(<ConnectorsSection {...p} />);
    fireEvent.click(screen.getByRole('button', { name: /Mail \(IMAP\)/ }));
    expect(document.querySelector('select')).toBeNull();
    fireEvent.click(screen.getByRole('radio', { name: 'STARTTLS' }));
    const pw = screen.getByLabelText('App password') as HTMLInputElement;
    expect(pw.type).toBe('password');
    fireEvent.change(pw, { target: { value: 'secret' } });
    fireEvent.click(screen.getAllByRole('button', { name: 'Connect' })[0]);
    await waitFor(() => expect(screen.getByText('Wrong password')).toBeTruthy());
    expect(p.onConnect).toHaveBeenCalledWith({
      connector: 'imap',
      name: '',
      values: { security: 'starttls', password: 'secret' },
    });
  });

  it('lists a connection with its error in words, checks and disconnects it', async () => {
    const conn = readConnectorConn({
      id: 'cx1',
      connector: 'rss',
      kind: 'feed',
      name: 'Example blog',
      account: 'Example blog',
      state: 'error',
      error: 'The feed answered 500',
    });
    const p = props({ connections: [conn] });
    render(<ConnectorsSection {...p} />);
    expect(screen.getByText('Example blog')).toBeTruthy();
    expect(screen.getByText('The feed answered 500')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Check' }));
    await waitFor(() => expect(p.onCheck).toHaveBeenCalledWith('cx1'));
    fireEvent.click(screen.getAllByRole('button', { name: 'Disconnect' })[0]);
    fireEvent.click(screen.getAllByRole('button', { name: 'Disconnect' })[1]);
    await waitFor(() => expect(p.onRemove).toHaveBeenCalledWith('cx1'));
  });

  it('shows the step a connection still waits for, with its link', () => {
    const conn = readConnectorConn({
      id: 'cx2',
      connector: 'telegram-bot',
      kind: 'chat',
      name: '@my_bot',
      account: '@my_bot',
      state: 'ok',
      next: {
        text: { en: 'Open the link in Telegram and press Start' },
        url: 'https://t.me/my_bot?start=abc',
      },
    });
    render(<ConnectorsSection {...props({ connections: [conn] })} />);
    expect(screen.getByText('Open the link in Telegram and press Start')).toBeTruthy();
    const link = screen.getByRole('link', { name: /Open the link/ });
    expect(link.getAttribute('href')).toBe('https://t.me/my_bot?start=abc');
  });

  it('shows the fields of the chosen way only, finds the calendars and connects the picked ones', async () => {
    const calendar = readSpec({
      id: 'caldav',
      kind: 'calendar',
      title: { en: 'Calendar' },
      fields: [
        {
          id: 'provider',
          type: 'choice',
          default: 'yandex',
          label: { en: 'Where' },
          choices: [
            { value: 'yandex', label: { en: 'Yandex' } },
            { value: 'ics', label: { en: 'Link .ics' } },
          ],
        },
        {
          id: 'username',
          type: 'text',
          label: { en: 'Login' },
          when: { field: 'provider', values: ['yandex'] },
        },
        {
          id: 'password',
          type: 'password',
          secret: true,
          label: { en: 'App password' },
          when: { field: 'provider', values: ['yandex'] },
        },
        {
          id: 'link',
          type: 'url',
          secret: true,
          label: { en: 'Secret address' },
          when: { field: 'provider', values: ['ics'] },
        },
        {
          id: 'calendars',
          type: 'choices',
          discover: true,
          optional: true,
          label: { en: 'Calendars' },
          when: { field: 'provider', values: ['yandex'] },
        },
      ],
    });
    const onDiscover = vi.fn(async () => [
      { value: '/c/work/', label: 'Work' },
      { value: '/c/home/', label: 'Home' },
    ]);
    const p = props({ catalog: [calendar], onDiscover });
    render(<ConnectorsSection {...p} />);
    fireEvent.click(screen.getByRole('button', { name: /Calendar/ }));
    expect(screen.queryByLabelText('Secret address')).toBeNull();
    fireEvent.change(screen.getByLabelText('Login'), { target: { value: 'me@example.com' } });
    fireEvent.change(screen.getByLabelText('App password'), { target: { value: 'pw' } });
    fireEvent.click(screen.getByRole('button', { name: 'Find' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Work' })).toBeTruthy());
    expect(onDiscover).toHaveBeenCalledWith({
      connector: 'caldav',
      field: 'calendars',
      values: { username: 'me@example.com', password: 'pw' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Work' }));
    expect(screen.getByRole('button', { name: 'Work' }).getAttribute('aria-pressed')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }));
    await waitFor(() =>
      expect(p.onConnect).toHaveBeenCalledWith({
        connector: 'caldav',
        name: '',
        values: { username: 'me@example.com', password: 'pw', calendars: '/c/work/' },
      }),
    );
    // The other way: the link, as a password field; the login is not sent.
    fireEvent.click(screen.getByRole('button', { name: /Calendar/ }));
    fireEvent.click(screen.getByRole('radio', { name: 'Link .ics' }));
    expect(screen.queryByLabelText('Login')).toBeNull();
    expect((screen.getByLabelText('Secret address') as HTMLInputElement).type).toBe('password');
  });

  it('does not ask a generated secret, shows it once and reissues it', async () => {
    const hook = readSpec({
      id: 'webhook',
      kind: 'webhook',
      title: { en: 'Incoming webhook' },
      fields: [
        {
          id: 'preset',
          type: 'choice',
          label: { en: 'Source' },
          default: 'github',
          choices: [{ value: 'github', label: { en: 'GitHub' } }],
        },
        { id: 'secret', type: 'password', secret: true, generate: true, label: { en: 'Secret' } },
      ],
    });
    const conn = readConnectorConn({
      id: 'cx1',
      connector: 'webhook',
      kind: 'webhook',
      name: 'GitHub',
      account: 'https://hub.example.com/api/platform/automations/hook/m/m1/cx1',
      state: 'ok',
      shown: [
        { label: { en: 'Address' }, value: 'https://hub.example.com/api/platform/automations/hook/m/m1/cx1' },
        { label: { en: 'Secret' }, value: 'abc123' },
      ],
    });
    const onReissue = vi.fn(async () => null);
    render(<ConnectorsSection {...props({ catalog: [hook], connections: [conn], onReissue })} />);
    fireEvent.click(screen.getByRole('button', { name: /Incoming webhook/ }));
    expect(screen.queryByLabelText('Secret', { selector: 'input[type="password"]' })).toBeNull();
    expect((screen.getByDisplayValue('abc123') as HTMLInputElement).readOnly).toBe(true);
    fireEvent.click(screen.getAllByRole('button', { name: 'Reissue' })[0]);
    const reissue = screen.getAllByRole('button', { name: 'Reissue' });
    fireEvent.click(reissue[reissue.length - 1]);
    await waitFor(() => expect(onReissue).toHaveBeenCalledWith('cx1'));
  });

  it('opens the rules of a connection and switches one, words under a rule that has them', async () => {
    const conn = readConnectorConn({
      id: 'cx9',
      connector: 'imap',
      kind: 'mail',
      name: 'Work mail',
      state: 'ok',
    });
    const draft = {
      id: 'mail-unanswered-draft',
      title: 'Draft a reply',
      summary: 'After 3 days',
      on: false,
      keywords: null,
    };
    const hw = { id: 'homework', title: 'An assignment', summary: 'Words below', on: true, keywords: ['ДЗ'] };
    const onRules = vi.fn(async () => [draft, hw]);
    const onSetRule = vi.fn(async () => [{ ...draft, on: true }, hw]);
    render(<ConnectorsSection {...props({ connections: [conn], ruled: ['imap'], onRules, onSetRule })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Rules' }));
    await waitFor(() => expect(screen.getByRole('switch', { name: 'Draft a reply' })).toBeTruthy());
    expect(screen.getByDisplayValue('ДЗ')).toBeTruthy();
    fireEvent.click(screen.getByRole('switch', { name: 'Draft a reply' }));
    await waitFor(() =>
      expect(screen.getByRole('switch', { name: 'Draft a reply' }).getAttribute('aria-checked')).toBe('true'),
    );
    expect(onSetRule).toHaveBeenCalledWith('cx9', { rule: 'mail-unanswered-draft', on: true });
  });

  it('shows no rules button for a connector without rules', () => {
    const conn = readConnectorConn({ id: 'cx8', connector: 'rss', kind: 'feed', name: 'Blog', state: 'ok' });
    render(
      <ConnectorsSection
        {...props({ connections: [conn], ruled: ['imap'], onRules: vi.fn(), onSetRule: vi.fn() })}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Rules' })).toBeNull();
  });
});
