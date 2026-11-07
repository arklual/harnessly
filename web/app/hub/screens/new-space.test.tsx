// @vitest-environment jsdom
// «Новый тенант» of an organization (GitHub issue 23): the screen makes the tenant in that organization, a failed press
// retried sends the same request id (so the hub hands back the tenant, never a second one), and it goes on to the
// tenant's machines there.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { t } from '../lib/i18n';
import { NewSpaceScreen } from './new-space';

const m = vi.hoisted(() => ({
  query: new URLSearchParams(),
  navigate: vi.fn(),
  api: {
    spaces: {
      create: vi.fn(),
      createInOrg: vi.fn(),
      update: vi.fn(),
      invite: vi.fn(),
      inviteLink: vi.fn(),
      bySlug: vi.fn(),
    },
  },
  reload: vi.fn(async () => {}),
}));

vi.mock('../lib/router', async (orig) => ({
  ...(await orig<typeof import('../lib/router')>()),
  navigate: m.navigate,
  useQuery: () => m.query,
}));
vi.mock('../lib/env', () => ({
  useApi: () => m.api,
  useEnv: () => ({ api: { auth: { logout: async () => {} } }, copyText: async () => true }),
}));
vi.mock('../lib/session', () => ({
  useMe: () => ({ user: { name: 'Anna', email: 'anna@example.com' }, spaces: [] }),
  useSession: () => ({ reload: m.reload, clear: () => {} }),
}));

const tenant = {
  id: 't1',
  slug: 'analytics',
  name: 'Аналитика',
  role: 'owner',
  createdAt: '',
  onboardingStep: 'machines',
};

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(cleanup);

const press = async (name: string) => {
  fireEvent.change(screen.getByRole('textbox', { name: /Название/ }), { target: { value: name } });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Дальше' }));
  });
};

describe('NewSpaceScreen in an organization', () => {
  it('makes the tenant in the organization and retries with the same request id', async () => {
    m.query = new URLSearchParams('org=acme');
    m.api.spaces.createInOrg
      .mockRejectedValueOnce(new Error('the answer was lost'))
      .mockResolvedValueOnce({ space: tenant });
    render(<NewSpaceScreen />);

    await press('Аналитика');
    expect(screen.getByText(t('err.internal'))).toBeTruthy();
    expect(m.navigate).not.toHaveBeenCalled();

    await press('Аналитика');
    const calls = m.api.spaces.createInOrg.mock.calls;
    expect(calls).toHaveLength(2);
    expect(calls[0][0]).toBe('acme');
    expect(calls[0][1]).toBe('Аналитика');
    expect(calls[1][2]).toBe(calls[0][2]);
    expect(String(calls[0][2]).length).toBeGreaterThanOrEqual(8);
    expect(m.api.spaces.create).not.toHaveBeenCalled();
    expect(m.api.spaces.update).not.toHaveBeenCalled();
    expect(m.navigate).toHaveBeenCalledWith('/o/acme/analytics/machines');
  });

  it('keeps the old path for a space of its own', async () => {
    m.query = new URLSearchParams();
    m.api.spaces.create.mockResolvedValueOnce({ space: { ...tenant, slug: 'mine' } });
    m.api.spaces.update.mockResolvedValueOnce({ space: { ...tenant, slug: 'mine' } });
    render(<NewSpaceScreen />);

    await press('Моё');
    expect(m.api.spaces.createInOrg).not.toHaveBeenCalled();
    expect(m.navigate).toHaveBeenCalledWith('/s/mine/setup/machines');
  });
});
