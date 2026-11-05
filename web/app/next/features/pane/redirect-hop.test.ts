import { describe, expect, it } from 'vitest';
import { followsRedirect, makeHops } from '@/next/features/pane/redirect-hop';

const sites = ['admin.example.ru', '*.preview.example.ru'];

describe('a redirect of the pane browser', () => {
  it('is followed from our https site to another of ours or a sign-in host, not elsewhere', () => {
    const from = 'https://admin.example.ru/events';
    const id = ['id.example.ru'];
    expect(followsRedirect('https://s1.preview.example.ru/', from, sites, [])).toBe(true);
    expect(followsRedirect('https://id.example.ru/realms/x/auth?client_id=a', from, sites, id)).toBe(true);
    expect(
      followsRedirect('https://admin.example.ru/cb?code=1', 'https://id.example.ru/login', sites, id),
    ).toBe(true);
    expect(followsRedirect('http://id.example.ru/', from, sites, id)).toBe(false);
    expect(followsRedirect('https://evil.example.com/', from, sites, id)).toBe(false);
    expect(followsRedirect('https://u:p@admin.example.ru/', from, sites, [])).toBe(false);
    expect(followsRedirect('javascript:alert(1)', from, sites, [])).toBe(false);
  });

  it('is not followed to a server of the machine, nor from a site that is not ours', () => {
    expect(followsRedirect('http://localhost:3000/', 'https://admin.example.ru/', sites, [])).toBe(false);
    expect(followsRedirect('https://localhost/', 'https://admin.example.ru/', sites, [])).toBe(false);
    expect(followsRedirect('https://admin.example.ru/', 'https://evil.example.com/', sites, [])).toBe(false);
    expect(followsRedirect('https://admin.example.ru/', null, sites, [])).toBe(false);
  });

  it('stops a loop after the limit, and starts again once the window passed', () => {
    const take = makeHops(3, 1000);
    expect([take(0), take(10), take(20), take(30)]).toEqual([true, true, true, false]);
    expect(take(1015)).toBe(true);
  });

  it('follows a Teleport application to a proxy of ours and back by their shape, whatever the app', () => {
    const app = 'https://posthog.tl.example.ru';
    const proxy = 'https://tl.example.ru';
    const ourProxy = ['tl.example.ru'];
    const launch = 'https://tl.example.ru:443/web/launch/posthog.tl.example.ru?path=%2Fproject%2F1&state=a_b';
    const back = `${app}/x-teleport-auth?state=a_b&subject=s#value=v`;
    expect(followsRedirect(launch, app, ourProxy, [])).toBe(true);
    expect(followsRedirect(back, proxy, ourProxy, [])).toBe(true);
    // A proxy that is not ours: the shape alone takes the tab nowhere.
    expect(followsRedirect(launch, app, [], [])).toBe(false);
    expect(followsRedirect('https://evil.ru/web/launch/x.evil.ru', 'https://x.evil.ru', [], [])).toBe(false);
    expect(followsRedirect('https://x.evil.ru/x-teleport-auth', 'https://evil.ru', [], [])).toBe(false);
    // Not another app's launcher, not the proxy's other pages, not a deeper host, another port or plain http.
    expect(followsRedirect('https://tl.example.ru/web/launch/grafana.tl.example.ru', app, [], [])).toBe(
      false,
    );
    expect(followsRedirect('https://tl.example.ru/web/login', app, [], [])).toBe(false);
    expect(
      followsRedirect(
        'https://tl.example.ru/web/launch/a.b.tl.example.ru',
        'https://a.b.tl.example.ru',
        [],
        [],
      ),
    ).toBe(false);
    expect(followsRedirect('https://posthog.tl.example.ru:8443/x-teleport-auth', proxy, ourProxy, [])).toBe(
      false,
    );
    expect(
      followsRedirect(
        'http://tl.example.ru/web/launch/posthog.tl.example.ru',
        'http://posthog.tl.example.ru',
        ourProxy,
        [],
      ),
    ).toBe(false);
    // The proxy's single sign-on: out to the sign-in host and back, between our sites.
    const id = ['id.example.ru'];
    expect(
      followsRedirect('https://id.example.ru/realms/x/auth?client_id=teleport', proxy, ourProxy, id),
    ).toBe(true);
    expect(
      followsRedirect(
        'https://tl.example.ru/v1/webapi/oidc/callback?code=1',
        'https://id.example.ru',
        ourProxy,
        id,
      ),
    ).toBe(true);
  });
});
