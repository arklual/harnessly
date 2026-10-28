import { describe, expect, it } from 'vitest';
import { siteNameOf, validSiteName } from '@/next/features/designly/publish/publish-api';

describe('siteNameOf', () => {
  it('makes a hub site name of a design title', () => {
    expect(siteNameOf('My Shop: v2!')).toBe('my-shop-v2');
    expect(siteNameOf('Лендинг')).toBe('');
    expect(siteNameOf('a'.repeat(50))).toHaveLength(30);
  });
});

describe('validSiteName', () => {
  it('takes what the hub takes', () => {
    expect(validSiteName('shop')).toBe(true);
    expect(validSiteName('my-shop-2')).toBe(true);
    expect(validSiteName('-shop')).toBe(false);
    expect(validSiteName('a--b')).toBe(false);
    expect(validSiteName('Shop')).toBe(false);
    expect(validSiteName('')).toBe(false);
  });
});
