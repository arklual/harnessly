// @vitest-environment jsdom
import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { postEfficiency, useEfficiency } from '@/hooks/use-efficiency';

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('useEfficiency on the hub', () => {
  it('reads the summary and posts the pulse from the organization endpoints of the hub', async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push(`${init?.method ?? 'GET'} ${url}`);
        if (url.endsWith('/api/orgs')) return json({ orgs: [{ id: 'o1', slug: 'acme' }] });
        return json({ scope: 'team', totals: { costUsd: null } });
      }),
    );
    const { result } = renderHook(() => useEfficiency<{ scope: string }>('/summary?scope=team'));
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(result.current.data?.scope).toBe('team');
    expect(await postEfficiency('/pulse', { speed: 3, quality: 3, friction: 3 })).toBeNull();
    expect(calls).toContain('GET /api/orgs/acme/efficiency/summary');
    expect(calls).toContain('POST /api/orgs/acme/efficiency/pulse');
  });
  it('asks the hub for the overview and falls back to the gateway when the hub says it is not there for this person', async () => {
    const calls: string[] = [];
    let hubStatus = 200;
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        calls.push(String(url));
        if (url.endsWith('/api/orgs')) return json({ orgs: [{ id: 'o1', slug: 'acme' }] });
        if (url.includes('/api/orgs/acme/efficiency/overview')) return json({ scope: 'hub' }, hubStatus);
        return json({ scope: 'gateway' });
      }),
    );
    const first = renderHook(() => useEfficiency<{ scope: string }>('/overview?scope=team&days=30'));
    await waitFor(() => expect(first.result.current.status).toBe('ready'));
    expect(first.result.current.data?.scope).toBe('hub');
    expect(calls).toContain('/api/orgs/acme/efficiency/overview?scope=team&days=30');
    first.unmount();

    hubStatus = 404;
    const second = renderHook(() => useEfficiency<{ scope: string }>('/overview?scope=me&days=30'));
    await waitFor(() => expect(second.result.current.status).toBe('ready'));
    expect(second.result.current.data?.scope).toBe('gateway');
  });
});
