import { describe, expect, it, vi } from 'vitest';
import {
  installPathMode,
  makeCookieJar,
  makeStorage,
  rewriteMarkup,
  rewriteSrcset,
  withPrefix,
  withoutPrefix,
} from './bridge-path';

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

describe('rewriteMarkup', () => {
  const mark = (raw: string) => `[${raw}]`;

  it('rewrites the address attributes of tags, quoted and not', () => {
    expect(
      rewriteMarkup(
        '<script src="/a.js"></script><img src=b.png alt=x><link href=\'c.css\' rel=stylesheet>',
        mark,
      ),
    ).toBe('<script src="[/a.js]"></script><img src=[b.png] alt=x><link href=\'[c.css]\' rel=stylesheet>');
    expect(rewriteMarkup('<FORM ACTION = "/go"><object data="x.svg"></object><div data="word">', mark)).toBe(
      '<FORM ACTION = "[/go]"><object data="[x.svg]"></object><div data="word">',
    );
    expect(rewriteMarkup('<img srcset="a.png 1x, data:image/png;base64,AA,BB 2x">', mark)).toBe(
      '<img srcset="[a.png] 1x, [data:image/png;base64,AA,BB] 2x">',
    );
  });

  it('leaves scripts, styles, comments, text and cut-off tags', () => {
    const script = '<script>var s = "<img src=/x.png>"; document.write("<a href=/y>")</script>';
    expect(rewriteMarkup(script, mark)).toBe(script);
    expect(rewriteMarkup('<style>a{background:url(/s.png)}</style><!-- <img src=/c> -->a src=/t', mark)).toBe(
      '<style>a{background:url(/s.png)}</style><!-- <img src=/c> -->a src=/t',
    );
    expect(rewriteMarkup('1 < 2 <img src="/half', mark)).toBe('1 < 2 <img src="/half');
    expect(rewriteMarkup('<script src="/a.js">var x = "<img src=/n>"</script><img src=/m>', mark)).toBe(
      '<script src="[/a.js]">var x = "<img src=/n>"</script><img src=[/m]>',
    );
  });
});

describe('rewriteSrcset', () => {
  it('reads candidates as the browser does', () => {
    expect(rewriteSrcset(' a.png 1x,b.png  2x , c.png', (r) => `/p/${r}`)).toBe(
      ' /p/a.png 1x,/p/b.png  2x , /p/c.png',
    );
    // Without a space the comma is the address's own, as the browser reads it.
    expect(rewriteSrcset('a.png,b.png, c.png', (r) => `/p/${r}`)).toBe('/p/a.png,b.png, /p/c.png');
  });
});

describe('document.write by path', () => {
  it('gives the written markup the prefix, relative to the page without it', () => {
    const write = vi.fn();
    const writeln = vi.fn();
    class FakeDocument {}
    Object.assign(FakeDocument.prototype, { write, writeln });
    const doc = Object.assign(new FakeDocument(), {
      baseURI: `${ORIGIN}/demo/index.html`,
    }) as unknown as Document;
    const win = {
      location: { origin: ORIGIN, pathname: '/demo/index.html', href: `${ORIGIN}/demo/index.html` },
      Document: FakeDocument,
    };
    installPathMode(win, doc, PREFIX);

    doc.write('<script src="/vendor/x.js"></script>', '<link rel=stylesheet href=./s.css>');
    expect(write).toHaveBeenCalledWith(
      `<script src="${ORIGIN}${PREFIX}/vendor/x.js"></script><link rel=stylesheet href=${ORIGIN}${PREFIX}/demo/s.css>`,
    );
    const kept = `<script src="https://cdn.example/x.js"></script><img src="${PREFIX}/y.png"><a href="#top">t</a><img src=data:image/png;base64,AA>`;
    doc.writeln(kept);
    expect(writeln).toHaveBeenCalledWith(kept);
    doc.write();
    expect(write).toHaveBeenLastCalledWith();
  });
});
