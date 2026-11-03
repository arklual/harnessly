// @vitest-environment jsdom
import { renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as config from '@/platform/config';
import { hubTarget, isSameOrigin, probeHubAtRoot, useHubEntry } from '@/next/features/sidebar/hub-entry';

const res = (init: { type?: string; ok?: boolean }) => ({ type: 'basic', ok: true, ...init }) as Response;

const withConfig = (hubUrl: string | null, hubLink: string | null) =>
  vi.spyOn(config, 'appConfig').mockReturnValue({ hubUrl, hubLink } as ReturnType<typeof config.appConfig>);

beforeEach(() => sessionStorage.clear());
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('hubTarget', () => {
  it('prefers the connected hub, takes the link otherwise, ignores a trailing slash', () => {
    expect(hubTarget({ hubUrl: 'https://h.example/', hubLink: 'https://l.example' })).toBe(
      'https://h.example',
    );
    expect(hubTarget({ hubUrl: null, hubLink: 'https://hub.example.com' })).toBe('https://hub.example.com');
    expect(hubTarget({ hubUrl: null, hubLink: 'https://hub.example.com/' })).toBe('https://hub.example.com');
    expect(hubTarget({ hubUrl: null, hubLink: null })).toBeNull();
  });
});

describe('isSameOrigin', () => {
  it('compares with the page origin', () => {
    expect(isSameOrigin('https://app.example.com', 'https://app.example.com')).toBe(true);
    expect(isSameOrigin('https://hub.example.com', 'https://app.example.com')).toBe(false);
  });
});

describe('probeHubAtRoot', () => {
  it('is false on a redirect, an error status or a failed request; true on 200', async () => {
    expect(await probeHubAtRoot(async () => res({ type: 'opaqueredirect', ok: false }))).toBe(false);
    expect(await probeHubAtRoot(async () => res({ ok: false }))).toBe(false);
    expect(await probeHubAtRoot(async () => Promise.reject(new Error('net')))).toBe(false);
    expect(await probeHubAtRoot(async () => res({}))).toBe(true);
  });
});

describe('useHubEntry', () => {
  it('shows nothing when no hub is configured', () => {
    withConfig(null, null);
    const { result } = renderHook(() => useHubEntry());
    expect(result.current).toBeNull();
  });

  it('shows a cross-origin hub at once, without a probe', () => {
    withConfig(null, 'https://hub.example.com');
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useHubEntry());
    expect(result.current).toBe('https://hub.example.com/');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('hides a same-origin hub when «/» redirects', async () => {
    withConfig(null, location.origin);
    const fetchMock = vi.fn(async () => res({ type: 'opaqueredirect', ok: false }));
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useHubEntry());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(result.current).toBeNull();
    expect(fetchMock).toHaveBeenCalledWith('/', expect.objectContaining({ redirect: 'manual' }));
  });

  it('shows a same-origin hub when «/» answers 200, and remembers it for the session', async () => {
    withConfig(null, location.origin);
    const fetchMock = vi.fn(async () => res({}));
    vi.stubGlobal('fetch', fetchMock);
    const first = renderHook(() => useHubEntry());
    await waitFor(() => expect(first.result.current).toBe(`${location.origin}/`));
    first.unmount();
    const second = renderHook(() => useHubEntry());
    expect(second.result.current).toBe(`${location.origin}/`);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
