import { describe, expect, it } from 'vitest';

import { isOutside, safeNext } from './router';

describe('safeNext', () => {
  it('keeps paths of this site, a machine page among them', () => {
    expect(safeNext('/s/acme')).toBe('/s/acme');
    expect(safeNext('/invite/abc?x=1')).toBe('/invite/abc?x=1');
    expect(safeNext('/m/abc/')).toBe('/m/abc/');
  });

  it('refuses everything that could leave the site or call the API', () => {
    for (const bad of [
      null,
      undefined,
      '',
      'https://evil.example',
      '//evil.example',
      '/\\evil.example',
      '/api/me',
      'x',
    ]) {
      expect(safeNext(bad)).toBeNull();
    }
  });
});

describe('isOutside', () => {
  it('tells the machine pages, which the hub answers itself, from the screens of the UI', () => {
    expect(isOutside('/m/abc/')).toBe(true);
    expect(isOutside('/m')).toBe(true);
    expect(isOutside('/s/acme')).toBe(false);
    expect(isOutside('/mail')).toBe(false);
    expect(isOutside('/')).toBe(false);
  });
});
