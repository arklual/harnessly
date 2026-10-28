import { describe, expect, it } from 'vitest';

import { bareAddress, SITE_NAME, slugName, trimRoot, type PickedFile } from './sites';

const pick = (path: string): PickedFile => ({ path, file: new File(['x'], 'f') });

describe('slugName', () => {
  it('makes a site name of a file name', () => {
    expect(slugName('My Shop v2.zip')).toBe('my-shop-v2');
    expect(slugName('--Дизайн--')).toBe('');
    expect(slugName('a'.repeat(40))).toHaveLength(30);
  });

  it('gives names the hub accepts', () => {
    expect(SITE_NAME.test(slugName('Landing Page.zip'))).toBe(true);
    expect(SITE_NAME.test('-bad')).toBe(false);
    expect(SITE_NAME.test('Bad')).toBe(false);
  });
});

describe('trimRoot', () => {
  it('cuts the one folder every path lies in', () => {
    const files = trimRoot([pick('dist/index.html'), pick('dist/a/b.js')]);
    expect(files.map((f) => f.path)).toEqual(['index.html', 'a/b.js']);
  });

  it('keeps paths that already start at the site', () => {
    const files = trimRoot([pick('index.html'), pick('a/b.js')]);
    expect(files.map((f) => f.path)).toEqual(['index.html', 'a/b.js']);
  });
});

describe('bareAddress', () => {
  it('drops the scheme and the closing slash', () => {
    expect(bareAddress('https://shop--acme.example.com/')).toBe('shop--acme.example.com');
  });
});
