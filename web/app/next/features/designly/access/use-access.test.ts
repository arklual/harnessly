// @vitest-environment jsdom
import { renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ACCESS, useProjectAccess } from '@/next/features/designly/access/use-access';

const fetchAccess = vi.hoisted(() => vi.fn());
vi.mock('@/next/features/designly/access/api', () => ({ fetchAccess }));

describe('useProjectAccess', () => {
  afterEach(() => fetchAccess.mockReset());

  // The runtime's /api/v2/me never names the account (its email is always null), so the page cannot tell a
  // standalone runtime from one behind the gateway: it always asks, and no gateway answers as absent.
  it('always asks the gateway and is absent where none answers', async () => {
    fetchAccess.mockResolvedValue(null);
    const { result } = renderHook(() => useProjectAccess('afisha'));
    await waitFor(() => expect(result.current.state).toEqual({ k: ACCESS.absent }));
    expect(fetchAccess).toHaveBeenCalledWith('afisha', expect.any(AbortSignal));
  });

  it('is ready with the gateway answer', async () => {
    const access = { team: 'none', canManage: true };
    fetchAccess.mockResolvedValue(access);
    const { result } = renderHook(() => useProjectAccess('afisha'));
    await waitFor(() => expect(result.current.state).toEqual({ k: ACCESS.ready, access }));
  });
});
