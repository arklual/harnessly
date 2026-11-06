import { describe, expect, it } from 'vitest';
import { withBase, withoutBase } from './base';

describe('the base path of the hub', () => {
  it('puts the base on a path of the hub, once', () => {
    expect(withBase('/api/me', '/harnessly')).toBe('/harnessly/api/me');
    expect(withBase('/harnessly/api/me', '/harnessly')).toBe('/harnessly/api/me');
    expect(withBase('/harnessly', '/harnessly')).toBe('/harnessly');
    expect(withBase('/harnessly?a=1', '/harnessly')).toBe('/harnessly?a=1');
    expect(withBase('/harnesslyx', '/harnessly')).toBe('/harnessly/harnesslyx');
  });

  it('leaves what is not a path of the hub', () => {
    for (const u of [
      'https://example.com/x',
      '//example.com/x',
      'relative/x',
      'data:text/plain,a',
      '#top',
      '',
    ]) {
      expect(withBase(u, '/harnessly')).toBe(u);
    }
  });

  it('is the identity at the root of a host', () => {
    expect(withBase('/api/me', '')).toBe('/api/me');
    expect(withoutBase('/s/team', '')).toBe('/s/team');
  });

  it('takes the base off a location', () => {
    expect(withoutBase('/harnessly', '/harnessly')).toBe('/');
    expect(withoutBase('/harnessly/s/team/machines', '/harnessly')).toBe('/s/team/machines');
    expect(withoutBase('/other/path', '/harnessly')).toBe('/other/path');
    expect(withoutBase('/harnesslyx', '/harnessly')).toBe('/harnesslyx');
  });
});
