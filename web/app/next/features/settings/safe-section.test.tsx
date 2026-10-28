// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createRef, type ReactNode } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { SecretAskHost } from '@/next/bindings/safe-asks';
import { SafePage } from '@/next/bindings/settings-safe';
import { setFetch } from '@/next/core/net/http';
import { readAsk, readSecret } from '@/next/features/safe/api';
import SafeSection, { type ISafeActions } from '@/next/features/settings/safe-section';
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
      if (init.method === 'PUT') {
        answered = true;
        return json(200, { secret: wire({ name: 'openai-key' }), created: true });
      }
      const asks = answered
        ? []
        : [
            {
              id: 'sa_1',
              name: 'openai-key',
              why: 'call the API',
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
    expect(screen.getByText(/Chat «Release notes»/)).toBeTruthy();
    const field = screen.getByLabelText('Value of «openai-key»') as HTMLInputElement;
    fireEvent.change(field, { target: { value: VALUE } });
    await act(async () => {
      fireEvent.click(screen.getByText('Put in the safe'));
    });
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.url).toContain('/api/platform/secrets/openai-key');
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
