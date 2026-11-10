import { describe, expect, it } from 'vitest';
import { addRecentRemoteUrl, normalizeRemoteUrl, parseAppConfig, resolveStartUrl, serverConfigOf } from './config';
import { applyDeepLink, findDeepLink, parseDeepLink } from './deeplink';
import { buildConnectPage } from './connect-page';
import { isLoopbackUrl, isSafeExternalUrl, isSameOrigin, isThemeSource } from './url-policy';
import { cascadeOffset, isVisibleOnAnyDisplay, restorePosition } from './window-state';
import { localPortFromEnv } from './local-runtime';

const screens = [{ id: 1, workArea: { x: 0, y: 0, width: 1920, height: 1080 } }];

describe('config', () => {
  it('survives broken json', () => {
    expect(parseAppConfig('{oops')).toEqual({});
    expect(parseAppConfig('[]')).toEqual({});
  });
  it('remote needs a url, else local', () => {
    expect(serverConfigOf({ server: { mode: 'remote' } })).toEqual({ mode: 'local' });
    expect(serverConfigOf({ server: { mode: 'remote', remoteUrl: 'https://a.example.com' } }).mode).toBe('remote');
  });
  it('normalizes addresses', () => {
    expect(normalizeRemoteUrl('hub.example.com')).toBe('https://hub.example.com');
    expect(normalizeRemoteUrl('http://localhost:8022/')).toBe('http://localhost:8022');
    expect(normalizeRemoteUrl('ftp://x.example.com')).toBeNull();
    expect(normalizeRemoteUrl('  ')).toBeNull();
  });
  it('keeps recent urls unique and capped', () => {
    let list: string[] = [];
    for (let i = 0; i < 12; i++) list = addRecentRemoteUrl(list, `https://h${i}.example.com`);
    list = addRecentRemoteUrl(list, 'https://h5.example.com');
    expect(list).toHaveLength(10);
    expect(list[0]).toBe('https://h5.example.com');
  });
  it('env url wins, local needs a port', () => {
    expect(resolveStartUrl({}, { HARNESSLY_DESKTOP_URL: 'hub.example.com' }, 8022)).toBe('https://hub.example.com');
    expect(resolveStartUrl({}, {}, 8022)).toBe('http://127.0.0.1:8022');
    expect(resolveStartUrl({}, {}, null)).toBeNull();
  });
  it('port from env', () => {
    expect(localPortFromEnv({})).toBe(8022);
    expect(localPortFromEnv({ HARNESSLY_DESKTOP_PORT: '9000' })).toBe(9000);
    expect(localPortFromEnv({ PORT: 'x' })).toBe(8022);
  });
});

describe('deep links', () => {
  it('maps to a path', () => {
    expect(parseDeepLink('harnessly://open/agents/docs/x?tab=1#h')).toBe('/agents/docs/x?tab=1#h');
    expect(applyDeepLink('https://hub.example.com/', 'harnessly://open/a')).toBe('https://hub.example.com/a');
  });
  it('rejects foreign hosts, traversal and other schemes', () => {
    expect(parseDeepLink('harnessly://evil.example.com/x')).toBeNull();
    expect(parseDeepLink('harnessly://open/..%2f..')).toBeNull();
    expect(parseDeepLink('harnessly://open//evil.example.com')).toBeNull();
    expect(parseDeepLink('https://open/x')).toBeNull();
    expect(parseDeepLink('nonsense')).toBeNull();
  });
  it('finds the link in argv', () => {
    expect(findDeepLink(['app', '--x', 'harnessly://open/y'])).toBe('harnessly://open/y');
    expect(findDeepLink(['app'])).toBeNull();
  });
});

describe('url policy', () => {
  it('blocks dangerous schemes', () => {
    expect(isSafeExternalUrl('https://example.com')).toBe(true);
    expect(isSafeExternalUrl('mailto:a@example.com')).toBe(true);
    for (const bad of ['javascript:alert(1)', 'data:text/html,x', 'file:///etc/passwd', 'nope', 5]) {
      expect(isSafeExternalUrl(bad)).toBe(false);
    }
  });
  it('loopback and origin checks', () => {
    expect(isLoopbackUrl('http://127.0.0.1:8022/x')).toBe(true);
    expect(isLoopbackUrl('https://example.com')).toBe(false);
    expect(isSameOrigin('https://a.example.com/x', 'https://a.example.com/y')).toBe(true);
    expect(isSameOrigin('https://a.example.com', 'https://b.example.com')).toBe(false);
    expect(isThemeSource('dark')).toBe(true);
    expect(isThemeSource('pink')).toBe(false);
  });
});

describe('window state', () => {
  it('keeps a visible position', () => {
    expect(restorePosition({ x: 100, y: 100, width: 800, height: 600 }, screens)).toEqual({ x: 100, y: 100 });
  });
  it('centres on the saved display when the old screen is gone', () => {
    expect(restorePosition({ x: 5000, y: 0, width: 800, height: 600, displayId: 1 }, screens)).toEqual({ x: 560, y: 240 });
    expect(restorePosition({ x: 5000, y: 0, width: 800, height: 600, displayId: 9 }, screens)).toEqual({});
    expect(isVisibleOnAnyDisplay({ x: 1900, y: 0, width: 800, height: 600 }, screens)).toBe(false);
  });
  it('cascades new windows', () => {
    expect(cascadeOffset(0)).toBe(0);
    expect(cascadeOffset(2)).toBe(64);
  });
});

describe('connect page', () => {
  it('escapes recent urls', () => {
    const html = buildConnectPage(['https://a.example.com/"><script>'], true);
    expect(html).not.toContain('"><script>');
    expect(html).toContain('#09090b');
  });
});
