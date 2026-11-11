// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createRef, type ReactNode } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import SafeAskDialog from '@/next/features/safe/ask-dialog';
import { SecretAskHost } from '@/next/bindings/safe-asks';
import { SafePage } from '@/next/bindings/settings-safe';
import { setFetch } from '@/next/core/net/http';
import { readAsk, readSecret } from '@/next/features/safe/api';
import SafeSection, { safeFoot, type ISafeActions } from '@/next/features/settings/safe-section';
import { pinLocale } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { PortalContext } from '@/next/ui/portal';

beforeAll(() => {
  pinLocale('en');
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  setFetch((url, init) => fetch(url, init));
});

const VALUE = 'gh' + 'p_' + 'Q7'.repeat(18);
const NOW = Date.UTC(2026, 9, 27, 12, 0, 0);
const MACHINE = 'local' as MachineId;

// A secret as GET <platform>/secrets answers it (cmd/runtime/routes_secrets.go).
const wire = (over: Record<string, unknown> = {}) => ({
  name: 'github-token',
  createdAt: NOW - 3 * 3600_000,
  updatedAt: NOW - 3 * 3600_000,
  lastUsedAt: NOW - 5 * 60_000,
  usedBy: [{ conv: 'c-1', title: 'Release notes', at: NOW - 5 * 60_000 }],
  ...over,
});

const host = createRef<HTMLDivElement>();
const inRoot = (ui: ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

const actions = (): ISafeActions & Record<string, ReturnType<typeof vi.fn>> => ({
  onPut: vi.fn(async () => null),
  onRemove: vi.fn(async () => null),
  onPatch: vi.fn(async () => null),
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

describe('the safe readers', () => {
  it('keep nothing of a value, even one sent by mistake', () => {
    const secret = readSecret(wire({ value: VALUE }));
    expect(JSON.stringify(secret)).not.toContain('Q7Q7');
    expect(() => readSecret({ name: 'x' })).toThrow();
    expect(
      readAsk({ id: 'sa_1', name: 'x', why: 'w', conv: '', title: '', replace: true, at: 1 }).replace,
    ).toBe(true);
  });
});

describe('Settings → Safe', { timeout: 20_000 }, () => {
  it('lists a secret by its name, when it was put and who used it', () => {
    render(<SafeSection secrets={[readSecret(wire())]} now={NOW} {...actions()} />);
    expect(screen.getByText('github-token')).toBeTruthy();
    expect(screen.getByText(/Added .* · used by «Release notes»/)).toBeTruthy();
  });

  it('says where each secret lives when the safe is on a server', () => {
    const store = { mode: 'vault', addr: 'https://openbao.example.com:8200', error: '' };
    render(
      <SafeSection
        secrets={[
          readSecret(wire({ where: 'vault' })),
          readSecret(wire({ name: 'old-token', where: 'local' })),
        ]}
        store={store}
        now={NOW}
        {...actions()}
      />,
    );
    expect(screen.getByText(/· on the server/)).toBeTruthy();
    expect(screen.getByText(/· on this machine/)).toBeTruthy();
    expect(screen.getByText(/kept on the server https:\/\/openbao\.example\.com:8200/)).toBeTruthy();
  });

  it('says a server safe that cannot be used, and why', () => {
    render(
      <SafeSection
        secrets={[]}
        store={{ mode: 'vault', addr: '', error: 'RUNTIME_SECRETS_VAULT_TOKEN_FILE is not set' }}
        now={NOW}
        {...actions()}
      />,
    );
    expect(
      screen.getByText(/settings do not work: RUNTIME_SECRETS_VAULT_TOKEN_FILE is not set/),
    ).toBeTruthy();
  });

  it('replaces a value and forgets the typed one at once', async () => {
    const a = actions();
    render(<SafeSection secrets={[readSecret(wire())]} now={NOW} {...a} />);
    fireEvent.click(screen.getByText('Replace'));
    const field = screen.getByLabelText('Value of «github-token»') as HTMLInputElement;
    expect(field.type).toBe('password');
    fireEvent.change(field, { target: { value: VALUE } });
    fireEvent.click(screen.getAllByText('Save')[0] as HTMLElement);
    expect(field.value).toBe('');
    await waitFor(() => expect(a.onPut).toHaveBeenCalledWith('github-token', VALUE));
  });

  it('removes only on the second press', async () => {
    const a = actions();
    render(<SafeSection secrets={[readSecret(wire())]} now={NOW} {...a} />);
    fireEvent.click(screen.getByText('Remove'));
    expect(a.onRemove).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('Remove for sure?'));
    await waitFor(() => expect(a.onRemove).toHaveBeenCalledWith('github-token'));
  });

  it('adds a secret with a valid, free name only', async () => {
    const a = actions();
    render(<SafeSection secrets={[readSecret(wire())]} now={NOW} {...a} />);
    fireEvent.click(screen.getByText('Add a secret'));
    const name = screen.getByPlaceholderText('github-token');
    fireEvent.change(name, { target: { value: 'github-token' } });
    expect(screen.getByText(/This name is taken/)).toBeTruthy();
    fireEvent.change(name, { target: { value: 'bad name' } });
    expect(screen.getByText(/Name: letters, digits/)).toBeTruthy();
    fireEvent.change(name, { target: { value: 'openai-key' } });
    fireEvent.change(screen.getByLabelText('Value'), { target: { value: VALUE } });
    const saves = screen.getAllByText('Save');
    fireEvent.click(saves[saves.length - 1] as HTMLElement);
    await waitFor(() => expect(a.onPut).toHaveBeenCalledWith('openai-key', VALUE));
  });

  it('clears the previous machine’s list and drafts before the next machine answers', async () => {
    let answer: ((response: Response) => void) | undefined;
    setFetch(async (url) =>
      url.includes('machine=remote')
        ? new Promise<Response>((resolve) => {
            answer = resolve;
          })
        : json(200, { secrets: [wire()], asks: [] }),
    );
    const view = render(<SafePage machine={MACHINE} now={NOW} />);
    await screen.findByText('github-token');
    fireEvent.click(screen.getByText('Edit access'));
    view.rerender(<SafePage machine={'remote' as MachineId} now={NOW} />);
    expect(screen.queryByText('github-token')).toBeNull();
    expect(screen.queryByRole('textbox', { name: /Origins/ })).toBeNull();
    await act(async () => {
      answer!(json(200, { secrets: [], asks: [] }));
    });
    expect(screen.getByText(/Nothing yet/)).toBeTruthy();
  });

  it('reads the machine and puts the value by PUT, the list never carrying it', async () => {
    const calls: { url: string; method: string; body: string }[] = [];
    setFetch(async (url, init) => {
      calls.push({ url, method: init.method ?? 'GET', body: String(init.body ?? '') });
      if (init.method === 'PUT')
        return json(200, { secret: wire({ name: 'openai-key', lastUsedAt: 0, usedBy: [] }), created: true });
      return json(200, { secrets: [wire()], asks: [] });
    });
    render(<SafePage machine={MACHINE} now={NOW} />);
    await screen.findByText('github-token');
    fireEvent.click(screen.getByText('Add a secret'));
    fireEvent.change(screen.getByPlaceholderText('github-token'), { target: { value: 'openai-key' } });
    fireEvent.change(screen.getByLabelText('Value'), { target: { value: VALUE } });
    const saves = screen.getAllByText('Save');
    fireEvent.click(saves[saves.length - 1] as HTMLElement);
    await screen.findByText('openai-key');
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.url).toContain('/api/platform/secrets/openai-key');
    expect(JSON.parse(put?.body ?? '{}')).toEqual({ value: VALUE });
  });
});

describe('the ask dialog', { timeout: 20_000 }, () => {
  it('shows an agent ask and sends the value to the machine, not anywhere else', async () => {
    const calls: { url: string; method: string; body: string }[] = [];
    let answered = false;
    setFetch(async (url, init) => {
      calls.push({ url, method: init.method ?? 'GET', body: String(init.body ?? '') });
      if (init.method === 'POST') {
        answered = true;
        return json(200, { ready: 'sa_1' });
      }
      const asks = answered
        ? []
        : [
            {
              id: 'sa_1',
              name: 'openai-key',
              why: 'call the API',
              origin: 'https://api.example.org',
              conv: 'c-1',
              title: 'Release notes',
              replace: false,
              at: NOW,
            },
          ];
      return json(200, { asks });
    });
    inRoot(<SecretAskHost machine={MACHINE} />);
    await screen.findByText('An agent asks for «openai-key»');
    expect(screen.getByText('call the API')).toBeTruthy();
    expect(screen.getByText('https://api.example.org')).toBeTruthy();
    expect(screen.getByText(/Chat «Release notes»/)).toBeTruthy();
    const field = screen.getByLabelText('Value of «openai-key»') as HTMLInputElement;
    fireEvent.change(field, { target: { value: VALUE } });
    await act(async () => {
      fireEvent.click(screen.getByText('Put in the safe'));
    });
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const put = calls.find((c) => c.method === 'POST');
    expect(put?.url).toContain('/api/platform/secret-asks/sa_1/put');
    expect(calls.filter((c) => c.body.includes('Q7Q7'))).toHaveLength(1);
    expect(field.value).toBe('');
  });

  it('declines with the button', async () => {
    const calls: { url: string; method: string }[] = [];
    setFetch(async (url, init) => {
      calls.push({ url, method: init.method ?? 'GET' });
      if (init.method === 'POST') return json(200, { declined: 'sa_2' });
      return json(200, {
        asks: [{ id: 'sa_2', name: 'aws-key', why: 'deploy', conv: '', title: '', replace: true, at: NOW }],
      });
    });
    inRoot(<SecretAskHost machine={MACHINE} />);
    await screen.findByText('An agent asks for a new value of «aws-key»');
    await act(async () => {
      fireEvent.click(screen.getByText('Decline'));
    });
    await waitFor(() =>
      expect(calls.some((c) => c.method === 'POST' && c.url.includes('/secret-asks/sa_2/decline'))).toBe(
        true,
      ),
    );
  });
});

describe('safe access metadata', () => {
  it('reads optional access and use origins while ignoring accidental values', () => {
    const secret = readSecret(
      wire({
        origins: ['https://example.org'],
        projects: ['p-1'],
        usedBy: [{ conv: 'c-1', title: 'Release', at: NOW, origin: 'https://example.org', value: VALUE }],
      }),
    );
    expect(secret.origins).toEqual(['https://example.org']);
    expect(secret.projects).toEqual(['p-1']);
    expect(secret.usedBy[0]?.origin).toBe('https://example.org');
    expect(JSON.stringify(secret)).not.toContain(VALUE);
    expect(readSecret(wire()).origins).toBeUndefined();
    expect(() => readSecret(wire({ origins: [3] }))).toThrow();
    expect(() => readSecret(wire({ projects: 'p-1' }))).toThrow();
    expect(() => readSecret(wire({ usedBy: [{ conv: '', title: '', at: 1, origin: 5 }] }))).toThrow();
    const ask = readAsk({
      id: 'a',
      name: 'x',
      conv: '',
      title: '',
      why: '',
      at: 1,
      origin: 'https://example.org',
      project: 'p-1',
      grant: true,
      value: VALUE,
    });
    expect(ask).toMatchObject({ origin: 'https://example.org', project: 'p-1', grant: true });
    expect(JSON.stringify(ask)).not.toContain(VALUE);
  });

  it('patches only metadata, preserves unknown project IDs and offers existing project names', async () => {
    const calls: { method: string; body: string; url: string }[] = [];
    const metadata = { origins: ['https://example.org'], projects: ['old-project'] };
    setFetch(async (url, init) => {
      calls.push({ method: init.method ?? 'GET', body: String(init.body ?? ''), url });
      if (init.method === 'PATCH') return json(200, { secret: wire({ ...JSON.parse(String(init.body)) }) });
      return json(200, { secrets: [wire(metadata)], asks: [] });
    });
    render(<SafePage machine={MACHINE} now={NOW} />);
    await screen.findByText('github-token');
    fireEvent.click(screen.getByText('Edit access'));
    const origins = screen.getByRole('textbox', { name: /Origins/ });
    expect(screen.queryByDisplayValue(VALUE)).toBeNull();
    fireEvent.change(origins, {
      target: { value: ' https://new.example.org,https://other.example.org\nhttps://new.example.org ' },
    });
    fireEvent.click(screen.getAllByText('Save')[0]!);
    await waitFor(() => expect(screen.queryByRole('textbox', { name: /Origins/ })).toBeNull());
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(patch?.url).toContain('/api/platform/secrets/github-token?machine=local');
    expect(JSON.parse(patch!.body)).toEqual({
      origins: ['https://new.example.org', 'https://other.example.org'],
      projects: ['old-project'],
    });
    expect(calls.some((c) => c.method === 'PUT')).toBe(false);
  });

  it('lets project choices add and revoke grants, including an empty list', async () => {
    const a = actions();
    render(
      <SafeSection
        secrets={[readSecret(wire({ projects: ['old-project'] }))]}
        now={NOW}
        projects={[{ id: 'p-1', name: 'Release project' }]}
        {...a}
      />,
    );
    fireEvent.click(screen.getByText('Edit access'));
    fireEvent.click(screen.getByLabelText('Release project'));
    fireEvent.click(screen.getByLabelText('old-project'));
    fireEvent.click(screen.getAllByText('Save')[0]!);
    await waitFor(() =>
      expect(a.onPatch).toHaveBeenCalledWith('github-token', { origins: [], projects: ['p-1'] }),
    );
    fireEvent.click(screen.getByText('Edit access'));
    fireEvent.click(screen.getByLabelText('old-project'));
    fireEvent.click(screen.getAllByText('Save')[0]!);
    await waitFor(() =>
      expect(a.onPatch).toHaveBeenCalledWith('github-token', { origins: [], projects: [] }),
    );
  });

  it('keeps refused metadata edits open and displays the refusal', async () => {
    const a = actions();
    vi.mocked(a.onPatch).mockResolvedValue('The origin was refused');
    render(<SafeSection secrets={[readSecret(wire())]} now={NOW} {...a} />);
    fireEvent.click(screen.getByText('Edit access'));
    fireEvent.click(screen.getAllByText('Save')[0]!);
    expect(await screen.findByText('The origin was refused')).toBeTruthy();
    expect(screen.getByRole('textbox', { name: /Origins/ })).toBeTruthy();
  });
});

describe('project grants', () => {
  it('never renders a value field for a grant and sends only the ask ID', async () => {
    const calls: { method: string; body: string; url: string }[] = [];
    setFetch(async (url, init) => {
      calls.push({ method: init.method ?? 'GET', body: String(init.body ?? ''), url });
      if (init.method === 'POST') return json(200, { ready: 'sa_grant' });
      return json(200, {
        asks: [
          {
            id: 'sa_grant',
            name: 'github-token',
            why: 'publish',
            conv: '',
            title: '',
            at: NOW,
            grant: true,
            project: 'p-1',
            origin: 'https://example.org',
          },
        ],
      });
    });
    inRoot(<SecretAskHost machine={MACHINE} />);
    await screen.findByText('Allow project p-1 to use github-token?');
    expect(screen.getByText('https://example.org')).toBeTruthy();
    expect(document.querySelector('input')).toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByText('Allow', { exact: true }));
    });
    await waitFor(() => expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1));
    const grant = calls.find((c) => c.method === 'POST')!;
    expect(grant.url).toContain('/secret-asks/sa_grant/grant?machine=local');
    expect(grant.body).toBe('');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('declines a grant, reports a refusal and allows retry without a value', async () => {
    const onGrant = vi.fn(async () => 'Access refused');
    const onDecline = vi.fn(async () => null);
    const onPut = vi.fn(async () => null);
    inRoot(
      <SafeAskDialog
        ask={readAsk({
          id: 'g',
          name: 'key',
          why: '',
          conv: '',
          title: '',
          at: NOW,
          grant: true,
          project: 'p',
        })}
        more={0}
        onGrant={onGrant}
        onPut={onPut}
        onDecline={onDecline}
        onLater={() => {}}
      />,
    );
    await act(async () => {
      fireEvent.click(screen.getByText('Allow', { exact: true }));
    });
    expect(screen.getByRole('alert').textContent).toBe('Access refused');
    expect(document.querySelector('input')).toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByText('Decline'));
    });
    expect(onDecline).toHaveBeenCalledOnce();
    expect(onPut).not.toHaveBeenCalled();
  });
});

describe('origin-only grants', () => {
  it.each(['en', 'ru'] as const)('shows an origin card without a value field in %s', async (locale) => {
    pinLocale(locale);
    const onGrant = vi.fn(async () => null);
    const onPut = vi.fn(async () => null);
    const ask = readAsk({
      id: 'site-card',
      name: 'service-key',
      why: 'sign in',
      conv: '',
      title: '',
      at: NOW,
      origin: 'https://example.org',
      originGrant: true,
      value: VALUE,
    });
    expect(ask.originGrant).toBe(true);
    expect(JSON.stringify(ask)).not.toContain(VALUE);
    inRoot(
      <SafeAskDialog
        ask={ask}
        more={0}
        onGrant={onGrant}
        onPut={onPut}
        onDecline={async () => null}
        onLater={() => undefined}
      />,
    );
    const title =
      locale === 'en'
        ? 'Allow service-key on site https://example.org?'
        : 'Разрешить service-key на сайте https://example.org?';
    expect(screen.getByRole('dialog', { name: title })).toBeTruthy();
    expect(screen.getByText(locale === 'en' ? 'Site' : 'Сайт', { exact: true })).toBeTruthy();
    expect(document.querySelectorAll('input, textarea')).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: locale === 'en' ? 'Allow' : 'Разрешить' }));
    await waitFor(() => expect(onGrant).toHaveBeenCalledWith(ask));
    expect(onPut).not.toHaveBeenCalled();
    pinLocale('en');
  });
});

describe('value request copy', () => {
  it.each([
    [
      'en',
      'https://example.org',
      'Password or code',
      'The agent will not see the value: it only gets "done" and enters it into a field on this site',
    ],
    [
      'ru',
      'https://example.org',
      'Пароль или код',
      'Агент не увидит значение: он получит только «готово» и введёт его в поле на этом сайте',
    ],
    [
      'en',
      '',
      'Paste the token or key',
      'The agent never sees the value: it gets only «ready» and puts the secret into its commands.',
    ],
    [
      'ru',
      '',
      'Вставьте токен или ключ',
      'Агент не увидит значение: он получит только «готово» и будет подставлять секрет в команды.',
    ],
  ] as const)('matches the site context in %s (%s)', (locale, origin, placeholder, note) => {
    pinLocale(locale);
    try {
      inRoot(
        <SafeAskDialog
          ask={readAsk({ id: 'value-card', name: 'login', why: '', conv: '', title: '', at: NOW, origin })}
          more={0}
          onGrant={async () => null}
          onPut={async () => null}
          onDecline={async () => null}
          onLater={() => undefined}
        />,
      );
      expect(screen.getByPlaceholderText(placeholder)).toBeTruthy();
      expect(screen.getByText(note, { exact: true })).toBeTruthy();
    } finally {
      pinLocale('en');
    }
  });
});

describe('where the safe says its values are', () => {
  it.each([
    ['en', null, 'Values are kept encrypted on this machine only, not in the hub, and never come back here.'],
    [
      'ru',
      null,
      'Значения хранятся в зашифрованном виде только на этой машине, не в хабе, и сюда не возвращаются.',
    ],
    [
      'en',
      { mode: 'hub', addr: '', error: '' },
      "Values are kept in the hub's safe, not on this machine, and never come back here.",
    ],
    [
      'en',
      { mode: 'vault', addr: 'https://bao.example.com', error: '' },
      'Values are kept on the server https://bao.example.com, not on this machine, and never come back here.',
    ],
  ] as const)('%s: %j', (locale, store, start) => {
    pinLocale(locale);
    try {
      expect(safeFoot(store).startsWith(start)).toBe(true);
    } finally {
      pinLocale('en');
    }
  });
});
