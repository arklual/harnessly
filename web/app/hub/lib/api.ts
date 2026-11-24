// The typed client of the hub's HTTP API (docs/hub/api.md). One function per endpoint; every call answers the JSON
// the contract names or throws an ApiError carrying the contract's error code. The screens decide by `code`, never by
// the message.
import type { NetworkPolicy } from './network';
import type { RolloutSwitch, SwitchKey, SwitchPut } from './switches';
import type {
  AppToken,
  DeviceRequest,
  Config,
  Invite,
  Machine,
  MachineCode,
  Me,
  Member,
  OnboardingStep,
  Passkey,
  Profile,
  Role,
  SignedIn,
  Site,
  SiteDeploy,
  SiteToken,
  Space,
  SpaceApp,
  SpaceRef,
  User,
} from './types';

export type ApiErrorCode =
  | 'invalid_request'
  | 'invalid_code'
  | 'unauthenticated'
  | 'forbidden'
  | 'cross_site'
  | 'signup_closed'
  | 'machine_policy'
  | 'not_found'
  | 'conflict'
  | 'account_exists'
  | 'expired'
  | 'provider_off'
  | 'rate_limited'
  | 'machine_offline'
  | 'machine_error'
  | 'machine_timeout'
  | 'client_api_off'
  | 'code_unknown'
  | 'internal'
  // Not the hub's: the request never got an answer.
  | 'network';

export class ApiError extends Error {
  readonly status: number;
  readonly code: ApiErrorCode;
  readonly details: Record<string, unknown>;

  constructor(status: number, code: ApiErrorCode, message: string, details: Record<string, unknown> = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.details = details;
  }

  /** `details.reason` of a 409 (login_running, last_method, last_owner, …). */
  get reason(): string | null {
    return typeof this.details.reason === 'string' ? this.details.reason : null;
  }

  /** Seconds to wait after a 429. */
  get retryAfter(): number | null {
    return typeof this.details.retryAfter === 'number' ? this.details.retryAfter : null;
  }

  /** Tries left after a wrong email code. */
  get attemptsLeft(): number | null {
    return typeof this.details.attemptsLeft === 'number' ? this.details.attemptsLeft : null;
  }
}

export const isApiError = (e: unknown, code?: ApiErrorCode): e is ApiError =>
  e instanceof ApiError && (code === undefined || e.code === code);

export type Fetcher = (input: string, init?: RequestInit) => Promise<Response>;

type Query = Record<string, string | number | boolean | undefined>;

const withQuery = (path: string, query?: Query): string => {
  if (!query) return path;
  const pairs = Object.entries(query).filter(
    (e): e is [string, string | number | boolean] => e[1] !== undefined,
  );
  if (pairs.length === 0) return path;
  return `${path}?${pairs.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`).join('&')}`;
};

const KNOWN_CODES: readonly string[] = [
  'invalid_request',
  'invalid_code',
  'unauthenticated',
  'forbidden',
  'cross_site',
  'signup_closed',
  'machine_policy',
  'not_found',
  'conflict',
  'account_exists',
  'expired',
  'provider_off',
  'rate_limited',
  'machine_offline',
  'machine_error',
  'machine_timeout',
  'client_api_off',
  'code_unknown',
  'internal',
];

const errorOf = async (res: Response): Promise<ApiError> => {
  type Body = { error?: unknown; message?: unknown; details?: unknown };
  const body = await res.json().then(
    (v: unknown) => (v && typeof v === 'object' ? (v as Body) : null),
    () => null,
  );
  const code = typeof body?.error === 'string' && KNOWN_CODES.includes(body.error) ? body.error : 'internal';
  const details =
    body?.details && typeof body.details === 'object' ? (body.details as Record<string, unknown>) : {};
  if (res.status === 429 && details.retryAfter === undefined) {
    const header = Number(res.headers.get('Retry-After'));
    if (Number.isFinite(header) && header > 0) details.retryAfter = header;
  }
  return new ApiError(
    res.status,
    code as ApiErrorCode,
    typeof body?.message === 'string' ? body.message : '',
    details,
  );
};

const enc = encodeURIComponent;

export const createApi = (fetcher: Fetcher = (input, init) => fetch(input, init)) => {
  const call = async <T>(
    method: string,
    path: string,
    body?: unknown,
    query?: Query,
    raw?: { body: BodyInit; type: string },
  ): Promise<T> => {
    const init: RequestInit = { method, credentials: 'same-origin', headers: { Accept: 'application/json' } };
    if (raw) {
      // A file is not JSON; the hub lets such a change in when the client names itself (docs/hub/sites.md).
      init.headers = { ...init.headers, 'Content-Type': raw.type, 'X-Harnessly-Client': 'hub-ui' };
      init.body = raw.body;
    } else if (method !== 'GET') {
      // The hub refuses a change that is not JSON (cross_site), also one with no body.
      init.headers = { ...init.headers, 'Content-Type': 'application/json' };
      if (body !== undefined) init.body = JSON.stringify(body);
    }
    let res: Response;
    try {
      res = await fetcher(withQuery(path, query), init);
    } catch {
      throw new ApiError(0, 'network', '');
    }
    if (!res.ok) throw await errorOf(res);
    if (res.status === 204) return undefined as T;
    try {
      return (await res.json()) as T;
    } catch {
      throw new ApiError(res.status, 'internal', '');
    }
  };
  const get = <T>(path: string, query?: Query) => call<T>('GET', path, undefined, query);
  const post = <T>(path: string, body: unknown = {}) => call<T>('POST', path, body);
  const patch = <T>(path: string, body: unknown) => call<T>('PATCH', path, body);
  const put = <T>(path: string, body: unknown) => call<T>('PUT', path, body);
  const del = <T = void>(path: string, query?: Query) => call<T>('DELETE', path, undefined, query);

  const site = (space: string, name: string) => `/api/sites/${enc(space)}/${enc(name)}`;

  return {
    config: () => get<Config>('/api/config'),

    /** The administrator's: the closed network's policy the machines take from the hub (docs/self-host/closed-network.md). */
    admin: {
      network: {
        get: () => get<NetworkPolicy>('/api/admin/network'),
        put: (policy: NetworkPolicy) => put<NetworkPolicy>('/api/admin/network', policy),
      },
      /** The rollout switches of the installation (docs/hub/rollout-switches.md). */
      switches: {
        list: () => get<{ switches: RolloutSwitch[] }>('/api/admin/switches'),
        put: (row: SwitchPut) => put<void>('/api/admin/switches', row),
        remove: (key: SwitchKey) => del('/api/admin/switches', key),
      },
    },

    sites: {
      list: (space: string) => get<{ sites: Site[] }>(`/api/sites/${enc(space)}`),
      create: (space: string, req: { name: string; access: Site['access'] }) =>
        post<Site>(`/api/sites/${enc(space)}`, req),
      get: (space: string, name: string) => get<Site>(site(space, name)),
      update: (space: string, name: string, req: { access?: Site['access']; spa?: boolean }) =>
        patch<Site>(site(space, name), req),
      remove: (space: string, name: string) => del(site(space, name)),
      begin: (space: string, name: string, files: Record<string, { hash: string; size: number }>) =>
        post<{ id: string; missing: string[] }>(`${site(space, name)}/deploys`, { files, source: 'upload' }),
      blob: (space: string, name: string, deploy: string, hash: string, data: Blob) =>
        call<void>(
          'PUT',
          `${site(space, name)}/deploys/${enc(deploy)}/blobs/${enc(hash)}`,
          undefined,
          undefined,
          {
            body: data,
            type: 'application/octet-stream',
          },
        ),
      commit: (space: string, name: string, deploy: string) =>
        post<SiteDeploy>(`${site(space, name)}/deploys/${enc(deploy)}/commit`, {}),
      upload: (space: string, name: string, zip: Blob) =>
        call<SiteDeploy>('POST', `${site(space, name)}/upload`, undefined, undefined, {
          body: zip,
          type: 'application/zip',
        }),
      setCurrent: (space: string, name: string, version: number) =>
        post<Site>(`${site(space, name)}/current`, { version }),
      tokens: (space: string, name: string) => get<{ tokens: SiteToken[] }>(`${site(space, name)}/tokens`),
      newToken: (space: string, name: string, tokenName: string) =>
        post<SiteToken>(`${site(space, name)}/tokens`, { name: tokenName }),
      removeToken: (space: string, name: string, id: string) => del(`${site(space, name)}/tokens/${enc(id)}`),
    },

    auth: {
      emailStart: (email: string, intent: 'signin' | 'signup') =>
        post<{ resendAfter: number; expiresIn: number }>('/api/auth/email/start', { email, intent }),
      emailVerify: (email: string, code: string) => post<SignedIn>('/api/auth/email/verify', { email, code }),
      passkeyBegin: () => post<{ challengeId: string; publicKey: unknown }>('/api/auth/passkey/login/begin'),
      passkeyFinish: (challengeId: string, credential: unknown) =>
        post<SignedIn>('/api/auth/passkey/login/finish', { challengeId, credential }),
      logout: async () => {
        await post<void>('/api/auth/logout');
        try {
          for (const key of Object.keys(localStorage))
            if (key.startsWith('hx-offline-person:')) localStorage.removeItem(key);
        } catch {
          /* Storage blocked. */
        }
      },
    },

    me: {
      get: () => get<Me>('/api/me'),
      rename: (name: string) => patch<{ user: User }>('/api/me', { name }),
      passkeyBegin: () => post<{ challengeId: string; publicKey: unknown }>('/api/me/passkeys/begin'),
      passkeyFinish: (challengeId: string, credential: unknown, name: string) =>
        post<{ passkey: Passkey }>('/api/me/passkeys/finish', { challengeId, credential, name }),
      removePasskey: (id: string) => del(`/api/me/passkeys/${enc(id)}`),
      removeIdentity: (provider: string) => del(`/api/me/identities/${enc(provider)}`),
      tokens: () => get<{ on: boolean; tokens: AppToken[] }>('/api/me/tokens'),
      revokeToken: (id: string) => del(`/api/me/tokens/${enc(id)}`),
    },

    device: {
      show: (code: string) => get<DeviceRequest>(`/api/auth/device?code=${enc(code)}`),
      approve: (code: string, allow: boolean, scopes: string[]) =>
        post<void>('/api/auth/device/approve', { code, allow, scopes }),
    },

    spaces: {
      bySlug: (slug: string) => get<{ space: Space }>(`/api/spaces/by-slug/${enc(slug)}`),
      update: (id: string, patchBody: { name?: string; onboardingStep?: OnboardingStep }) =>
        patch<{ space: Space }>(`/api/spaces/${enc(id)}`, patchBody),
      members: (id: string) => get<{ members: Member[] }>(`/api/spaces/${enc(id)}/members`),
      /** The apps installed for the space, from the hub's store: they open at the hub with every machine off. */
      apps: (id: string) => get<{ apps: SpaceApp[] }>(`/api/spaces/${enc(id)}/apps`),
      setRole: (id: string, userId: string, role: Role) =>
        patch<{ member: Member }>(`/api/spaces/${enc(id)}/members/${enc(userId)}`, { role }),
      removeMember: (id: string, userId: string) => del(`/api/spaces/${enc(id)}/members/${enc(userId)}`),
      /** A role other than member (a co-owner) goes only when asked, so the old body stays as it was. */
      invite: (id: string, emails: string[], role: Role = 'member', teamId?: string) =>
        post<{ invites: Invite[]; joined?: string[] }>(`/api/spaces/${enc(id)}/invites`, {
          emails,
          ...(role === 'member' ? {} : { role }),
          ...(teamId ? { teamId } : {}),
        }),
      inviteLink: (id: string, opts: { maxUses?: number; days?: number; teamId?: string } = {}) =>
        post<{ invite: Invite }>(`/api/spaces/${enc(id)}/invites/link`, opts),
      invites: (id: string) => get<{ invites: Invite[] }>(`/api/spaces/${enc(id)}/invites`),
      revokeInvite: (id: string, inviteId: string) => del(`/api/spaces/${enc(id)}/invites/${enc(inviteId)}`),
    },

    invites: {
      peek: (token: string) =>
        get<{
          spaceName: string;
          inviterName: string;
          kind: 'email' | 'link';
          email: string | null;
          valid: boolean;
          /** The team the invitation also puts the person in. */
          teamName?: string;
        }>(`/api/invites/${enc(token)}`),
      accept: (token: string) => post<{ space: SpaceRef }>(`/api/invites/${enc(token)}/accept`),
    },

    machines: {
      list: (spaceId: string) => get<{ machines: Machine[] }>(`/api/spaces/${enc(spaceId)}/machines`),
      issueCode: (spaceId: string, profile: Profile, confirmFull: boolean) =>
        post<MachineCode>(`/api/spaces/${enc(spaceId)}/machines/codes`, {
          profile,
          ...(confirmFull ? { confirmFull: true } : {}),
        }),
      claimLocal: (spaceId: string) =>
        post<{ machine: Machine }>(`/api/spaces/${enc(spaceId)}/machines/local`),
      // path: an address under the machine's page to open (`/m/<id>/…`), else its start.
      open: (id: string, path?: string) =>
        get<{ url: string }>(
          `/api/machines/${enc(id)}/open${path ? `?${new URLSearchParams({ path }).toString()}` : ''}`,
        ),
    },
  };
};

export type HubApi = ReturnType<typeof createApi>;

/** Where the sign-in with a provider starts: a page navigation, not a fetch. */
export const oauthStartUrl = (
  provider: string,
  intent: 'signin' | 'signup' | 'link',
  next?: string,
): string => withQuery(`/api/auth/oauth/${enc(provider)}/start`, { intent, next });

/** The URL of a space's event stream. */
