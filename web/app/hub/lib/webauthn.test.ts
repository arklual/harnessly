import { describe, expect, it } from 'vitest';

import { creationOptions, fromBase64Url, requestOptions, toBase64Url } from './webauthn';

describe('webauthn JSON', () => {
  it('round-trips base64url without padding', () => {
    const bytes = Uint8Array.from([251, 255, 254, 0, 1, 2, 3]);
    const text = toBase64Url(bytes);
    expect(text).not.toMatch(/[+/=]/);
    expect(Array.from(fromBase64Url(text))).toEqual(Array.from(bytes));
  });

  it('turns the challenge and credential ids of a request into buffers', () => {
    const o = requestOptions({
      challenge: 'AQID',
      rpId: 'hub.example.com',
      userVerification: 'preferred',
      allowCredentials: [{ type: 'public-key', id: 'BAUG' }],
    });
    expect(Array.from(new Uint8Array(o.challenge as ArrayBuffer))).toEqual([1, 2, 3]);
    expect(o.rpId).toBe('hub.example.com');
    expect(Array.from(new Uint8Array(o.allowCredentials?.[0]?.id as ArrayBuffer))).toEqual([4, 5, 6]);
  });

  it('turns the user id of a creation into a buffer and keeps the rest', () => {
    const o = creationOptions({
      challenge: 'AQID',
      rp: { name: 'harnessly', id: 'hub.example.com' },
      user: { id: 'BwgJ', name: 'a@b.ru', displayName: 'A' },
      pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
    });
    expect(Array.from(new Uint8Array(o.user.id as ArrayBuffer))).toEqual([7, 8, 9]);
    expect(o.user.name).toBe('a@b.ru');
    expect(o.excludeCredentials).toBeUndefined();
  });
});
