// Passkeys: the hub speaks WebAuthn JSON (base64url buffers, as go-webauthn emits and reads); the browser wants
// ArrayBuffers. These are the two conversions and the two ceremonies.

export const fromBase64Url = (text: string): Uint8Array<ArrayBuffer> => {
  const padded = text.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (text.length % 4)) % 4);
  const bin = atob(padded);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
};

export const toBase64Url = (buf: ArrayBuffer | Uint8Array): string => {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
};

interface JsonDescriptor {
  id: string;
  type: PublicKeyCredentialType;
  transports?: AuthenticatorTransport[];
}

type RequestJson = Omit<PublicKeyCredentialRequestOptions, 'challenge' | 'allowCredentials'> & {
  challenge: string;
  allowCredentials?: JsonDescriptor[];
};

type CreationJson = Omit<PublicKeyCredentialCreationOptions, 'challenge' | 'user' | 'excludeCredentials'> & {
  challenge: string;
  user: Omit<PublicKeyCredentialUserEntity, 'id'> & { id: string };
  excludeCredentials?: JsonDescriptor[];
};

const descriptors = (list: JsonDescriptor[] | undefined): PublicKeyCredentialDescriptor[] | undefined =>
  list?.map((d) => ({ ...d, id: fromBase64Url(d.id) }));

export const requestOptions = (json: unknown): PublicKeyCredentialRequestOptions => {
  const o = json as RequestJson;
  return { ...o, challenge: fromBase64Url(o.challenge), allowCredentials: descriptors(o.allowCredentials) };
};

export const creationOptions = (json: unknown): PublicKeyCredentialCreationOptions => {
  const o = json as CreationJson;
  return {
    ...o,
    challenge: fromBase64Url(o.challenge),
    user: { ...o.user, id: fromBase64Url(o.user.id) },
    excludeCredentials: descriptors(o.excludeCredentials),
  };
};

/** `PublicKeyCredential.toJSON()` where the browser has it; otherwise the same shape built by hand. */
export const credentialJson = (cred: PublicKeyCredential): unknown => {
  const native = (cred as { toJSON?: () => unknown }).toJSON;
  if (typeof native === 'function') return native.call(cred);
  const res = cred.response;
  const base = {
    id: cred.id,
    rawId: toBase64Url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
  };
  if ('authenticatorData' in res) {
    const a = res as AuthenticatorAssertionResponse;
    return {
      ...base,
      response: {
        clientDataJSON: toBase64Url(a.clientDataJSON),
        authenticatorData: toBase64Url(a.authenticatorData),
        signature: toBase64Url(a.signature),
        userHandle: a.userHandle ? toBase64Url(a.userHandle) : undefined,
      },
    };
  }
  const r = res as AuthenticatorAttestationResponse;
  return {
    ...base,
    response: {
      clientDataJSON: toBase64Url(r.clientDataJSON),
      attestationObject: toBase64Url(r.attestationObject),
      transports: typeof r.getTransports === 'function' ? r.getTransports() : [],
    },
  };
};

export const passkeysSupported = (): boolean =>
  typeof window !== 'undefined' &&
  typeof window.PublicKeyCredential === 'function' &&
  !!navigator.credentials;

export type PasskeyCeremony = 'cancelled' | 'failed';

/** Asks the authenticator to sign the hub's challenge. Resolves null when the person closed the system dialog. */
export const getPasskey = async (publicKey: unknown): Promise<unknown | null> => {
  try {
    const cred = await navigator.credentials.get({ publicKey: requestOptions(publicKey) });
    return cred ? credentialJson(cred as PublicKeyCredential) : null;
  } catch (e) {
    if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) return null;
    throw e;
  }
};

export const createPasskey = async (publicKey: unknown): Promise<unknown | null> => {
  try {
    const cred = await navigator.credentials.create({ publicKey: creationOptions(publicKey) });
    return cred ? credentialJson(cred as PublicKeyCredential) : null;
  } catch (e) {
    if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) return null;
    throw e;
  }
};
