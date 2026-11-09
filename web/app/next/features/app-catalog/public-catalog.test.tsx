// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import PublicCatalog from '@/next/features/app-catalog/public-catalog';
import { SEARCH_DELAY_MS, usePublicCatalog } from '@/next/features/app-catalog/use-public';
import type { IPublicEntry } from '@/next/features/app-catalog/api';
import type { MachineId } from '@/next/protocol/v2';

vi.mock('@/next/core/net/runtime-json', async (orig) => ({
  ...(await orig<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: vi.fn(),
}));
import { runtimeJson, RuntimeError } from '@/next/core/net/runtime-json';

const M = 'm1' as MachineId;
const asked = vi.mocked(runtimeJson);

const entry = (over: Partial<IPublicEntry> = {}): IPublicEntry => ({
  id: 'acme.demo',
  version: '1.0.0',
  name: 'Демо',
  description: 'Что оно делает',
  trust: 'sandboxed',
  permissions: ['storage', 'chat.prompt'],
  sum: 'abc',
  by: 'ann',
  ...over,
});

// The search is the apps page's header; the page here has a field that stands for it.
const Page = ({ installed = [], onChanged = vi.fn() }: { installed?: string[]; onChanged?: () => void }) => {
  const catalog = usePublicCatalog(M, onChanged);
  return (
    <>
      <input type="search" onChange={(e) => catalog.setQuery(e.target.value)} />
      <PublicCatalog catalog={catalog} installed={installed} />
    </>
  );
};

beforeEach(() => {
  asked.mockReset();
  vi.useFakeTimers({ shouldAdvanceTime: true });
});
afterEach(() => {
  vi.useRealTimers();
  cleanup();
});

const settle = () => act(async () => void (await vi.advanceTimersByTimeAsync(SEARCH_DELAY_MS + 10)));

describe('PublicCatalog', () => {
  it('lists the apps and opens the install screen with the warning and the access before anything is installed', async () => {
    const access = [
      {
        key: 'perm:chat.prompt',
        code: 'chat.prompt',
        class: 'medium',
        label: { ru: 'Писать агенту', en: 'Message the agent' },
      },
      {
        key: 'secret:TOKEN',
        code: 'secrets:TOKEN',
        class: 'high',
        label: { ru: 'Использовать секрет TOKEN', en: 'Use the secret TOKEN' },
        threat: { ru: 'Сможет действовать от вашего имени.', en: 'It can act on your behalf.' },
      },
    ];
    asked.mockResolvedValue({ apps: [entry({ check: { refusals: [], secrets: [], access } as never })] });
    render(<Page />);
    await settle();
    expect(screen.getByText('Демо')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /^(Install|Установить)$/ }));
    expect(screen.getByText(/(We do not check apps|Мы не проверяем приложения)/)).toBeTruthy();
    expect(screen.getByText(/^(Use the secret TOKEN|Использовать секрет TOKEN)$/)).toBeTruthy();
    // The dangerous line comes first.
    const items = [...document.querySelectorAll('.hx-access__item')].map((li) => li.textContent ?? '');
    expect(items[0]).toMatch(/TOKEN/);
    // Nothing was sent to install yet: only the list was read.
    expect(asked.mock.calls.every(([, , init]) => init?.method !== 'POST')).toBe(true);
  });

  it('installs the entry that was shown, by its sum, and reads the list again', async () => {
    const onChanged = vi.fn();
    asked.mockResolvedValue({ apps: [entry()] });
    render(<Page onChanged={onChanged} />);
    await settle();
    fireEvent.click(screen.getAllByRole('button')[0]!);
    fireEvent.click(screen.getAllByRole('button').at(-1)!);
    await waitFor(() => expect(onChanged).toHaveBeenCalled());
    const post = asked.mock.calls.find(([, , init]) => init?.method === 'POST');
    expect(post?.[1]).toContain('/apps/public/install');
    expect(JSON.parse(String(post?.[2]?.body))).toEqual({ id: 'acme.demo', sum: 'abc' });
  });

  it('shows the words of a refused install and offers no button for what is installed', async () => {
    asked.mockImplementation(async (_m, _p, init) => {
      if (init?.method === 'POST') throw new RuntimeError('changed');
      return { apps: [entry(), entry({ id: 'acme.other', name: 'Другое' })] };
    });
    render(<Page installed={['acme.other']} />);
    await settle();
    expect(screen.getAllByRole('button')).toHaveLength(1);
    fireEvent.click(screen.getAllByRole('button')[0]!);
    fireEvent.click(screen.getAllByRole('button').at(-1)!);
    await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy());
  });

  it('searches after the person stops typing', async () => {
    asked.mockResolvedValue({ apps: [] });
    render(<Page />);
    await settle();
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'карт' } });
    await settle();
    expect(String(asked.mock.calls.at(-1)?.[1])).toContain('/apps/public?q=');
  });
});
