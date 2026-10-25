import { describe, expect, it } from 'vitest';
import { isProxyUrl } from '@/next/features/settings/accounts-section';

describe('isProxyUrl', () => {
  it('takes an http(s) address with a host', () => {
    expect(isProxyUrl('https://llm.example.com')).toBe(true);
    expect(isProxyUrl('http://127.0.0.1:8080/v1')).toBe(true);
  });

  it('refuses what the CLI cannot send requests to', () => {
    expect(isProxyUrl('')).toBe(false);
    expect(isProxyUrl('llm.example.com')).toBe(false);
    expect(isProxyUrl('ftp://llm.example.com')).toBe(false);
    expect(isProxyUrl('https://user:pass@llm.example.com')).toBe(false);
    expect(isProxyUrl('https://')).toBe(false);
  });
});
