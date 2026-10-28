import { createDecipheriv, createPrivateKey, createPublicKey, diffieHellman, hkdfSync } from 'node:crypto';
import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { canSeal, fromBase64, seal, SealError, toBase64 } from './seal';
import type { Envelope } from './types';

const PKCS8 = '302e020100300506032b656e04220420';

// The machine side of the contract written out with node:crypto (what Go's crypto/ecdh + crypto/hkdf do): an
// independent reading of the algorithm in docs/hub/api.md, so a mistake in seal.ts cannot hide in a round trip.
const open = (
  encPrivate: Uint8Array,
  encKey: string,
  machineId: string,
  purpose: string,
  e: Envelope,
): string => {
  const priv = createPrivateKey({
    key: Buffer.concat([Buffer.from(PKCS8, 'hex'), Buffer.from(encPrivate)]),
    format: 'der',
    type: 'pkcs8',
  });
  const epk = Buffer.from(fromBase64(e.epk));
  const pub = createPublicKey({
    key: Buffer.concat([Buffer.from('302a300506032b656e032100', 'hex'), epk]),
    format: 'der',
    type: 'spki',
  });
  const shared = diffieHellman({ privateKey: priv, publicKey: pub });
  const salt = Buffer.concat([epk, Buffer.from(fromBase64(encKey))]);
  const key = Buffer.from(hkdfSync('sha256', shared, salt, 'harnessly-envelope-v1', 32));
  const ct = Buffer.from(fromBase64(e.ct));
  const decipher = createDecipheriv('aes-256-gcm', key, Buffer.from(fromBase64(e.nonce)));
  decipher.setAAD(Buffer.from(`harnessly:${machineId}:${purpose}`));
  decipher.setAuthTag(ct.subarray(ct.length - 16));
  return Buffer.concat([decipher.update(ct.subarray(0, ct.length - 16)), decipher.final()]).toString('utf8');
};

const machineKeys = () => {
  const seed = crypto.getRandomValues(new Uint8Array(32));
  const priv = createPrivateKey({
    key: Buffer.concat([Buffer.from(PKCS8, 'hex'), Buffer.from(seed)]),
    format: 'der',
    type: 'pkcs8',
  });
  const spki = createPublicKey(priv).export({ format: 'der', type: 'spki' });
  return { seed, encKey: toBase64(new Uint8Array(spki.subarray(spki.length - 32))) };
};

describe('seal', () => {
  it('is supported where the tests run', async () => {
    expect(await canSeal()).toBe(true);
  });

  it('opens on the machine side with the same machine and purpose', async () => {
    const { seed, encKey } = machineKeys();
    const e = await seal(encKey, 'abcdefghijklmnopqrst', 'key:claude', 'sk-ant-секрет');
    expect(e.v).toBe(1);
    expect(fromBase64(e.epk)).toHaveLength(32);
    expect(fromBase64(e.nonce)).toHaveLength(12);
    expect(open(seed, encKey, 'abcdefghijklmnopqrst', 'key:claude', e)).toBe('sk-ant-секрет');
  });

  it.each([
    ['another machine', 'bbbbbbbbbbbbbbbbbbbb', 'key:claude'],
    ['another purpose', 'abcdefghijklmnopqrst', 'key:codex'],
  ])('does not open as %s', async (_name, machineId, purpose) => {
    const { seed, encKey } = machineKeys();
    const e = await seal(encKey, 'abcdefghijklmnopqrst', 'key:claude', 'secret');
    expect(() => open(seed, encKey, machineId, purpose, e)).toThrow();
  });

  it('draws a fresh ephemeral key and nonce every time', async () => {
    const { encKey } = machineKeys();
    const a = await seal(encKey, 'abcdefghijklmnopqrst', 'env:TOKEN', 'x');
    const b = await seal(encKey, 'abcdefghijklmnopqrst', 'env:TOKEN', 'x');
    expect(a.epk).not.toBe(b.epk);
    expect(a.nonce).not.toBe(b.nonce);
    expect(a.ct).not.toBe(b.ct);
  });

  it('refuses a plaintext over 8 KiB and a key of the wrong length', async () => {
    const { encKey } = machineKeys();
    await expect(seal(encKey, 'm', 'env:A', 'x'.repeat(8 * 1024 + 1))).rejects.toMatchObject({
      reason: 'too_long',
    });
    await expect(seal(toBase64(new Uint8Array(31)), 'm', 'env:A', 'x')).rejects.toBeInstanceOf(SealError);
    await expect(seal(encKey, 'm', 'env:A', 'x'.repeat(8 * 1024))).resolves.toBeTruthy();
  });
});

// The vectors of internal/hubproto/testdata/envelope.json (made by the Go side): the same ephemeral key and nonce
// must give the same envelope. They land with the agent's branch; until the file exists in this tree the test is
// skipped rather than faked.
interface Vector {
  machineId: string;
  purpose: string;
  encPrivate: string;
  encKey: string;
  ephemeralPrivate: string;
  nonce: string;
  plaintext: string;
  envelope: Envelope;
}

const vectorsPath = fileURLToPath(
  new URL('../../../../internal/hubproto/testdata/envelope.json', import.meta.url),
);
const vectors: Vector[] = existsSync(vectorsPath)
  ? (JSON.parse(readFileSync(vectorsPath, 'utf8')) as Vector[])
  : [];

describe.skipIf(vectors.length === 0)('seal against the Go vectors', () => {
  it.each(vectors.map((v, i) => [i, v] as const))('reproduces vector %i', async (_i, v) => {
    const e = await seal(v.encKey, v.machineId, v.purpose, v.plaintext, {
      ephemeralPrivate: fromBase64(v.ephemeralPrivate),
      nonce: fromBase64(v.nonce),
    });
    expect(e).toEqual(v.envelope);
    expect(open(fromBase64(v.encPrivate), v.encKey, v.machineId, v.purpose, e)).toBe(v.plaintext);
  });
});
