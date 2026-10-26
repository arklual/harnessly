import { describe, expect, it } from 'vitest';
import { HOME, ROUTE, SETTINGS_SECTIONS, parseRoute, placeKeyOf, routePath } from '@/next/routes/routes';

describe('routes', () => {
  it('reads a chat address and writes it back the same', () => {
    expect(parseRoute('/c/tab-12')).toEqual({ kind: ROUTE.chat, id: 'tab-12' });
    expect(parseRoute('/c/agent:runner-2:tab-1?x=1')).toEqual({
      kind: ROUTE.chat,
      id: 'agent:runner-2:tab-1',
    });
    expect(routePath({ kind: ROUTE.chat, id: 'agent:runner-2:tab-1' })).toBe('/c/agent:runner-2:tab-1');
    expect(routePath({ kind: ROUTE.chat, id: 'a b/c' })).toBe('/c/a%20b%2Fc');
    expect(parseRoute(routePath({ kind: ROUTE.chat, id: 'a b/c' }))).toEqual({
      kind: ROUTE.chat,
      id: 'a b/c',
    });
  });

  it('reads the screens of «More» and writes them back the same', () => {
    expect(parseRoute('/stats')).toEqual({ kind: ROUTE.stats });
    expect(parseRoute('/efficiency/')).toEqual({ kind: ROUTE.efficiency });
    expect(routePath({ kind: ROUTE.stats })).toBe('/stats');
    expect(routePath({ kind: ROUTE.efficiency })).toBe('/efficiency');
  });

  it('reads the Apps screen with or without the closing slash and writes it back the same', () => {
    expect(parseRoute('/apps')).toEqual({ kind: ROUTE.apps });
    expect(parseRoute('/apps/?tab=gitlab')).toEqual({ kind: ROUTE.apps });
    expect(routePath({ kind: ROUTE.apps })).toBe('/apps');
    expect(parseRoute('/apps/extra')).toEqual(HOME);
  });

  it('reads Settings with or without a section and writes it back the same', () => {
    expect(parseRoute('/settings')).toEqual({ kind: ROUTE.settings, section: null });
    expect(parseRoute('/settings/')).toEqual({ kind: ROUTE.settings, section: null });
    expect(parseRoute('/settings/machines')).toEqual({ kind: ROUTE.settings, section: 'machines' });
    expect(parseRoute('/settings/claude/?x=1')).toEqual({ kind: ROUTE.settings, section: 'claude' });
    expect(parseRoute('/settings/models')).toEqual({ kind: ROUTE.settings, section: 'accounts' });
    expect(routePath({ kind: ROUTE.settings, section: null })).toBe('/settings');
    expect(routePath({ kind: ROUTE.settings, section: 'skills' })).toBe('/settings/skills');
    for (const section of SETTINGS_SECTIONS) {
      const route = { kind: ROUTE.settings, section } as const;
      expect(parseRoute(routePath(route))).toEqual(route);
    }
    // A section the screen does not have, and a path below one, are the greeting.
    expect(parseRoute('/settings/nope')).toEqual(HOME);
    expect(parseRoute('/settings/claude/extra')).toEqual(HOME);
  });

  it('anything else is the greeting', () => {
    expect(parseRoute('/')).toEqual(HOME);
    expect(parseRoute('/c/')).toEqual(HOME);
    expect(parseRoute('/c/%E0%A4%A')).toEqual(HOME);
    expect(routePath(HOME)).toBe('/');
  });

  it('reads a design with the chat it was opened from and writes it back the same', () => {
    expect(parseRoute('/designly/abc')).toEqual({ kind: ROUTE.site, id: 'abc' });
    expect(parseRoute('/designly/abc?chat=tab-7')).toEqual({ kind: ROUTE.site, id: 'abc', chat: 'tab-7' });
    expect(routePath({ kind: ROUTE.site, id: 'abc', chat: 'agent:r:tab-1' })).toBe(
      '/designly/abc?chat=agent%3Ar%3Atab-1',
    );
    expect(parseRoute(routePath({ kind: ROUTE.site, id: 'abc', chat: 'agent:r:tab-1' }))).toEqual({
      kind: ROUTE.site,
      id: 'abc',
      chat: 'agent:r:tab-1',
    });
  });
});

describe('the access addresses of a design', () => {
  it('reads an invitation under both prefixes and writes it back', () => {
    expect(parseRoute('/designly/invite/tok_12345678')).toEqual({
      kind: ROUTE.invite,
      token: 'tok_12345678',
    });
    expect(parseRoute('/design/invite/tok_12345678/')).toEqual({ kind: ROUTE.invite, token: 'tok_12345678' });
    expect(routePath({ kind: ROUTE.invite, token: 'tok_12345678' })).toBe('/designly/invite/tok_12345678');
  });

  it('takes a guest-link push address that reached the app (an old gateway) for the greeting', () => {
    expect(parseRoute('/design/guest/aaaaaaaaaaaaaaaaaaaaaa')).toEqual({ kind: ROUTE.home });
    expect(parseRoute('/designly/guest/aaaaaaaaaaaaaaaaaaaaaa/')).toEqual({ kind: ROUTE.home });
  });

  it('keeps ?access= of a design next to ?chat=', () => {
    expect(parseRoute('/design/p1?access=requests')).toEqual({
      kind: ROUTE.site,
      id: 'p1',
      access: 'requests',
    });
    expect(parseRoute('/designly/p1?access=nope')).toEqual({ kind: ROUTE.site, id: 'p1' });
    expect(routePath({ kind: ROUTE.site, id: 'p1', access: 'people' })).toBe('/designly/p1?access=people');
    expect(routePath({ kind: ROUTE.site, id: 'p1', chat: 'tab-1', access: 'people' })).toBe(
      '/designly/p1?chat=tab-1&access=people',
    );
  });
});

describe('placeKeyOf', () => {
  it('a chat is one place whatever its query, hash, slash or escapes', () => {
    const key = 'chat:agent:runner-2:tab-1';
    for (const path of [
      '/c/agent:runner-2:tab-1',
      '/c/agent:runner-2:tab-1/',
      '/c/agent%3Arunner-2%3Atab-1?pane=1&x=2',
      '/c/agent:runner-2:tab-1#top',
    ])
      expect(placeKeyOf(path)).toBe(key);
  });

  it('a design is one place, with its chat when opened with one', () => {
    expect(placeKeyOf('/designly/afisha')).toBe('design:afisha');
    expect(placeKeyOf('/design/afisha/?page=2&board=b')).toBe('design:afisha');
    expect(placeKeyOf('/designly/afisha?access=people')).toBe('design:afisha');
    expect(placeKeyOf('/designly/afisha?chat=tab-1&page=2')).toBe('design:afisha+chat:tab-1');
    expect(placeKeyOf('/designly/afisha?chat=tab-2')).not.toBe(placeKeyOf('/designly/afisha?chat=tab-1'));
  });

  it('the library, settings, the start page and the screens of More are one place each', () => {
    expect(placeKeyOf('/designly/')).toBe('designly');
    expect(placeKeyOf('/settings')).toBe('settings');
    expect(placeKeyOf('/settings/claude?x=1')).toBe('settings');
    expect(placeKeyOf('/')).toBe('home');
    expect(placeKeyOf('/?c=1')).toBe('home');
    expect(placeKeyOf('/apps/')).toBe('apps');
    expect(placeKeyOf('/stats')).toBe('stats');
  });

  it('an address that is none of ours is its plain pathname', () => {
    expect(placeKeyOf('/somewhere/else/?a=1#b')).toBe('/somewhere/else');
    expect(placeKeyOf('/caf%C3%A9/')).toBe('/café');
  });
});
