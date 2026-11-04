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
});
