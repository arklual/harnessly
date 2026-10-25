import { describe, expect, it } from 'vitest';
import { isPublicAddress } from '@/next/features/designly/api';

describe('isPublicAddress', () => {
  it('is an address another device can open', () => {
    expect(isPublicAddress('https://harnessly.example.org/designly/x')).toBe(true);
    expect(isPublicAddress('http://192.168.1.20:8080/designly/x')).toBe(true);
  });

  it("is not the machine's own loopback, nor a broken address", () => {
    expect(isPublicAddress('http://localhost:5173/designly/x')).toBe(false);
    expect(isPublicAddress('http://app.localhost/designly/x')).toBe(false);
    expect(isPublicAddress('http://127.0.0.1:8080/')).toBe(false);
    expect(isPublicAddress('http://[::1]:8080/')).toBe(false);
    expect(isPublicAddress('not an address')).toBe(false);
  });
});
