// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { setFetch } from '@/next/core/net/http';
import { setApiOrigin } from '@/next/core/net/machine';
import { changeProfile, loadConversationProfiles, loadProfiles } from './profiles-api';
const fetcher = vi.fn();
beforeEach(() => {
  setApiOrigin({ base: '/agents' });
  setFetch(fetcher);
  fetcher.mockReset();
});
afterEach(() => {
  setApiOrigin({ base: '' });
  setFetch((url, init) => fetch(url, init));
});
it('routes profile reads to the selected machine and encodes conversation IDs', async () => {
  const current = { id: 'one', name: 'One', color: '#112233', kind: 'managed' };
  fetcher.mockResolvedValue(Response.json({ profiles: [current], current, hasTabs: true }));
  expect(await loadConversationProfiles('remote & machine', 'chat/a?b')).toEqual({
    profiles: [current],
    current,
    hasTabs: true,
  });
  expect(fetcher.mock.calls[0]?.[0]).toBe(
    '/agents/api/agent-browser/profiles?conv=chat%2Fa%3Fb&machine=remote%20%26%20machine',
  );
});
it('preserves error text and announces only successful changes on the matching machine', async () => {
  const changed = vi.fn();
  window.addEventListener('browser-profiles-changed', changed);
  try {
    fetcher.mockResolvedValueOnce(Response.json({ error: 'Profile is running elsewhere' }, { status: 409 }));
    await expect(changeProfile('m', { action: 'delete', id: 'p' })).rejects.toThrow(
      'Profile is running elsewhere',
    );
    expect(changed).not.toHaveBeenCalled();
    fetcher.mockResolvedValueOnce(Response.json({ ok: true }));
    await changeProfile('m', {
      action: 'answer',
      askId: 'a',
      id: '',
      name: 'Work',
      color: '#123456',
      scope: 'chat',
    });
    expect(changed).toHaveBeenCalledOnce();
    expect((changed.mock.calls[0]![0] as CustomEvent).detail).toBe('m');
    expect(JSON.parse(fetcher.mock.calls[1]![1].body as string)).toMatchObject({
      action: 'answer',
      askId: 'a',
      id: '',
      scope: 'chat',
      name: 'Work',
    });
  } finally {
    window.removeEventListener('browser-profiles-changed', changed);
  }
});
it('rejects malformed pages and mutation acknowledgements instead of showing stale success', async () => {
  fetcher.mockResolvedValueOnce(Response.json({ profiles: {} }));
  await expect(loadProfiles('m')).rejects.toThrow();
  fetcher.mockResolvedValueOnce(Response.json({ ok: false }));
  await expect(changeProfile('m', { action: 'clear', id: 'p' })).rejects.toThrow();
});
it('normalizes empty Go slices while retaining project access', async () => {
  fetcher.mockResolvedValueOnce(
    Response.json({
      profiles: [
        {
          id: 'p',
          name: 'P',
          color: '#112233',
          kind: 'managed',
          createdAt: 1,
          lastUsedAt: 0,
          usedBy: null,
          path: '/data/p',
          running: false,
          projects: null,
        },
      ],
      projects: { demo: { default: 'p', granted: null } },
      asks: null,
      noSandbox: false,
    }),
  );
  const data = await loadProfiles('m');
  expect(data.profiles[0]?.usedBy).toEqual([]);
  expect(data.projects.demo).toEqual({ default: 'p', granted: [] });
});
it('returns an import warning from the page API', async () => {
  fetcher.mockResolvedValueOnce(
    Response.json({ ok: true, warning: 'Some saved logins could not be copied' }),
  );
  await expect(
    changeProfile('m', { action: 'import', name: 'Imported', color: '#112233', path: '/data/chrome' }),
  ).resolves.toEqual({ warning: 'Some saved logins could not be copied' });
});
