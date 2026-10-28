// Seals a key or a secret in the browser to the machine's X25519 key (docs/hub/api.md, "Sealed envelope"): the hub
// only relays the ciphertext. X25519 + HKDF-SHA256 + AES-256-GCM through WebCrypto, so no library is needed.
//
//   shared = X25519(ephemeralPrivate, encKey)
//   key    = HKDF-SHA256(ikm = shared, salt = epk ‖ encKey, info = "harnessly-envelope-v1", 32 bytes)
//   ct     = AES-256-GCM(key, nonce, plaintext, aad = "harnessly:<machineId>:<purpose>")
import type { Envelope } from './types';

const INFO = 'harnessly-envelope-v1';
const MAX_PLAINTEXT = 8 * 1024;
// The PKCS#8 wrapper of a raw X25519 private key (RFC 8410): WebCrypto imports private keys only in this form.
const PKCS8_PREFIX = Uint8Array.from([
  0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x04, 0x22, 0x04, 0x20,
]);

export class SealError extends Error {
  readonly reason: 'unsupported' | 'too_long' | 'bad_key';

  constructor(reason: SealError['reason']) {
    super(reason);
    this.name = 'SealError';
    this.reason = reason;
  }
}

export const toBase64 = (bytes: Uint8Array): string => {
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
};

export const fromBase64 = (text: string): Uint8Array<ArrayBuffer> => {
  const bin = atob(text.replace(/-/g, '+').replace(/_/g, '/'));
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
};

const utf8 = (text: string): Uint8Array<ArrayBuffer> => new TextEncoder().encode(text);

export interface SealFixture {
  /** The raw 32-byte ephemeral private key (tests reproduce a vector with it; production draws a fresh pair). */
  ephemeralPrivate?: Uint8Array<ArrayBuffer>;
  /** The 12-byte nonce (same purpose). */
  nonce?: Uint8Array<ArrayBuffer>;
}

const subtleOf = (): SubtleCrypto => {
  const subtle = globalThis.crypto?.subtle;
  if (!subtle) throw new SealError('unsupported');
  return subtle;
};

const ephemeralPair = async (
  subtle: SubtleCrypto,
  fixed?: Uint8Array<ArrayBuffer>,
): Promise<{ priv: CryptoKey; epk: Uint8Array<ArrayBuffer> }> => {
  if (fixed) {
    const pkcs8 = new Uint8Array(PKCS8_PREFIX.length + fixed.length);
    pkcs8.set(PKCS8_PREFIX);
    pkcs8.set(fixed, PKCS8_PREFIX.length);
    const priv = await subtle.importKey('pkcs8', pkcs8, { name: 'X25519' }, true, ['deriveBits']);
    // A private OKP key exports with its public half in `x`.
    const jwk = await subtle.exportKey('jwk', priv);
    if (!jwk.x) throw new SealError('bad_key');
    return { priv, epk: fromBase64(jwk.x) };
  }
  const pair = (await subtle.generateKey({ name: 'X25519' }, false, ['deriveBits'])) as CryptoKeyPair;
  return { priv: pair.privateKey, epk: new Uint8Array(await subtle.exportKey('raw', pair.publicKey)) };
};

/** Seals `plaintext` for one machine and one purpose (`key:<provider>` or `env:<NAME>`). */
export const seal = async (
  encKey: string,
  machineId: string,
  purpose: string,
  plaintext: string,
  fixture: SealFixture = {},
): Promise<Envelope> => {
  const subtle = subtleOf();
  const message = utf8(plaintext);
  if (message.length > MAX_PLAINTEXT) throw new SealError('too_long');
  const recipientRaw = fromBase64(encKey);
  if (recipientRaw.length !== 32) throw new SealError('bad_key');
  try {
    const recipient = await subtle.importKey('raw', recipientRaw, { name: 'X25519' }, false, []);
    const { priv, epk } = await ephemeralPair(subtle, fixture.ephemeralPrivate);
    const shared = await subtle.deriveBits({ name: 'X25519', public: recipient }, priv, 256);
    const salt = new Uint8Array(epk.length + recipientRaw.length);
    salt.set(epk);
    salt.set(recipientRaw, epk.length);
    const ikm = await subtle.importKey('raw', shared, 'HKDF', false, ['deriveKey']);
    const key = await subtle.deriveKey(
      { name: 'HKDF', hash: 'SHA-256', salt, info: utf8(INFO) },
      ikm,
      { name: 'AES-GCM', length: 256 },
      false,
      ['encrypt'],
    );
    const nonce = fixture.nonce ?? crypto.getRandomValues(new Uint8Array(12));
    const ct = await subtle.encrypt(
      { name: 'AES-GCM', iv: nonce, additionalData: utf8(`harnessly:${machineId}:${purpose}`) },
      key,
      message,
    );
    return { v: 1, epk: toBase64(epk), nonce: toBase64(nonce), ct: toBase64(new Uint8Array(ct)) };
  } catch (e) {
    if (e instanceof SealError) throw e;
    // The browser has no X25519 in WebCrypto (older than Chrome 133, Firefox 129, Safari 17).
    throw new SealError('unsupported');
  }
};

let supported: Promise<boolean> | null = null;

/** Whether this browser can seal at all (a secure context and X25519 in WebCrypto); asked once. */
export const canSeal = (): Promise<boolean> => {
  supported ??= (async () => {
    try {
      const subtle = subtleOf();
      await subtle.generateKey({ name: 'X25519' }, false, ['deriveBits']);
      return true;
    } catch {
      return false;
    }
  })();
  return supported;
};
