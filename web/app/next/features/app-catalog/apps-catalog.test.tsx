// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AppsCatalog from '@/next/features/app-catalog/apps-catalog';
import { useTeam } from '@/next/features/app-catalog/use-team';
import type { ICatalogEntry, ITeamInfo } from '@/next/features/app-catalog/api';
import type { MachineId } from '@/next/protocol/v2';

vi.mock('@/next/core/net/runtime-json', async (orig) => ({
  ...(await orig<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: vi.fn(),
}));
import { runtimeJson, RuntimeError } from '@/next/core/net/runtime-json';

const M = 'm1' as MachineId;
const asked = vi.mocked(runtimeJson);

const entry = (over: Partial<ICatalogEntry> = {}): ICatalogEntry => ({
  id: 'acme.demo',
  channel: 'stable',
  version: '1.0.0',
  name: 'Демо',
  description: 'Что оно делает',
  trust: 'sandboxed',
  permissions: [],
  by: 'ann',
  installed: false,
  ...over,
});

const Page = ({ onChanged }: { onChanged: () => void }) => <AppsCatalog team={useTeam(M, onChanged)} />;

/** A machine that keeps the team in memory and answers the page's calls the way the runtime does. */
const machine = (start: ITeamInfo) => {
  let info = start;
  const posts: { path: string; body: unknown }[] = [];
  asked.mockImplementation(async (_m, path, init) => {
    if (init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as { id: string; mute?: boolean };
      posts.push({ path, body });
      if (path.endsWith('/install')) {
        if (info.role !== 'owner') throw new RuntimeError('not-owner');
        info = {
          ...info,
          catalog: info.catalog.map((e) => (e.id === body.id ? { ...e, installed: true } : e)),
        };
      }
      if (path.endsWith('/mute')) info = { ...info, muted: body.mute ? [body.id] : [] };
      return {};
    }
    return info;
  });
  return posts;
};

beforeEach(() => asked.mockReset());
afterEach(cleanup);

describe('AppsCatalog', () => {
  it('draws nothing for a machine in no team', async () => {
    machine({ space: '', role: '', catalog: [], muted: [] });
    const { container } = render(<Page onChanged={vi.fn()} />);
    await act(async () => {});
    expect(container.firstChild).toBeNull();
  });

  it('lets an owner install for the team, and waits for the machine before it shows the change', async () => {
    const posts = machine({ space: 'sp1', role: 'owner', catalog: [entry()], muted: [] });
    const changed = vi.fn();
    render(<Page onChanged={changed} />);
    const install = await screen.findByRole('button', {
      name: /^(Поставить|Install)$/,
    });
    expect(screen.getByText('Демо')).toBeTruthy();

    // Hold the machine's answer: the row is busy and shows no change yet.
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const was = asked.getMockImplementation()!;
    asked.mockImplementationOnce(async (...a) => {
      await gate;
      return was(...a);
    });
    fireEvent.click(install);
    await waitFor(() => expect(screen.getByRole('button').getAttribute('aria-busy')).toBe('true'));
    expect(screen.getByText('Демо')).toBeTruthy();
    expect(changed).not.toHaveBeenCalled();

    // The team has the app now: it is among the person's apps, not in the catalog.
    await act(async () => release());
    await waitFor(() => expect(screen.queryByText('Демо')).toBeNull());
    expect(posts).toEqual([
      { path: expect.stringContaining('/apps/team/install'), body: { id: 'acme.demo', channel: 'stable' } },
    ]);
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it('gives a member no install, and keeps what the team has out of the catalog', async () => {
    machine({
      space: 'sp1',
      role: 'member',
      catalog: [entry(), entry({ id: 'acme.two', name: 'Два', installed: true })],
      muted: [],
    });
    render(<Page onChanged={vi.fn()} />);
    await screen.findByText('Демо');
    expect(screen.queryByText('Два')).toBeNull();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('says why a change was refused and leaves the row as it was', async () => {
    machine({ space: 'sp1', role: 'owner', catalog: [entry()], muted: [] });
    render(<Page onChanged={vi.fn()} />);
    const install = await screen.findByRole('button', {
      name: /^(Поставить|Install)$/,
    });
    const was = asked.getMockImplementation()!;
    asked.mockImplementation(async (m, path, init) => {
      if (init?.method === 'POST') throw new RuntimeError('trusted');
      return was(m, path, init);
    });
    fireEvent.click(install);
    expect((await screen.findByRole('alert')).textContent).toBe('trusted');
    expect(screen.getByRole('button', { name: /^(Поставить|Install)$/ })).toBeTruthy();
  });
});
