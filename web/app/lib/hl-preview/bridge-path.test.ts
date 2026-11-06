import { describe, expect, it, vi } from 'vitest';
import { makeCookieJar, makeStorage, withPrefix, withoutPrefix } from './bridge-path';

const PREFIX = '/agents/__pv/1-0123456789abcdef0123456789abcdef';
const ORIGIN = 'https://h.example';
const BASE = `${ORIGIN}/dashboard`;

describe('withPrefix', () => {
  it('puts the prefix before root-absolute addresses of the host', () => {
    expect(withPrefix('/api/x?y=1', PREFIX, BASE, ORIGIN)).toBe(`${ORIGIN}${PREFIX}/api/x?y=1`);
    expect(withPrefix('api/x', PREFIX, BASE, ORIGIN)).toBe(`${ORIGIN}${PREFIX}/api/x`);
    expect(withPrefix(`${ORIGIN}/x`, PREFIX, BASE, ORIGIN)).toBe(`${ORIGIN}${PREFIX}/x`);
    expect(withPrefix('wss://h.example/ws', PREFIX, BASE, ORIGIN)).toBe(`wss://h.example${PREFIX}/ws`);
  });

  it('leaves other hosts and addresses that have the prefix', () => {
    expect(withPrefix('https://cdn.example/x.js', PREFIX, BASE, ORIGIN)).toBe('https://cdn.example/x.js');
    expect(withPrefix('//cdn.example/x.js', PREFIX, BASE, ORIGIN)).toBe('//cdn.example/x.js');
    expect(withPrefix(`${PREFIX}/x`, PREFIX, BASE, ORIGIN)).toBe(`${ORIGIN}${PREFIX}/x`);
    expect(withPrefix('data:text/plain,a', PREFIX, BASE, ORIGIN)).toBe('data:text/plain,a');
  });
});

describe('withoutPrefix', () => {
  it('gives the page its localhost path', () => {
    expect(withoutPrefix(`${PREFIX}/users/7`, PREFIX)).toBe('/users/7');
    expect(withoutPrefix(PREFIX, PREFIX)).toBe('/');
    expect(withoutPrefix('/other', PREFIX)).toBe('/other');
  });
});

describe('makeStorage', () => {
  it('behaves as Storage and reports writes', () => {
    const changed = vi.fn();
    const s = makeStorage({ a: '1' }, changed);
    expect(s.getItem('a')).toBe('1');
    expect(s.getItem('b')).toBeNull();
    s.setItem('b', 2 as unknown as string);
    expect(s.getItem('b')).toBe('2');
    expect(s.length).toBe(2);
    expect(s.key(1)).toBe('b');
    s.removeItem('a');
    expect(JSON.stringify(s)).toBe('{"b":"2"}');
    s.clear();
    expect(s.length).toBe(0);
    expect(changed).toHaveBeenCalledTimes(3);
  });
});

describe('makeCookieJar', () => {
  it('reads and writes document.cookie, honours expiry', () => {
    const saved = vi.fn();
    const jar = makeCookieJar({ seen: '1' }, saved);
    jar.set('token=abc; Path=/; SameSite=Lax');
    expect(jar.get()).toBe('seen=1; token=abc');
    jar.set('seen=; Max-Age=0');
    expect(jar.get()).toBe('token=abc');
    jar.set('token=x; Expires=Thu, 01 Jan 1970 00:00:00 GMT');
    expect(jar.get()).toBe('');
    expect(saved).toHaveBeenLastCalledWith({});
  });
});
