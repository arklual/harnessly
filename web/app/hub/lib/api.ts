// The typed client of the hub's HTTP API (docs/hub/api.md). One function per endpoint; every call answers the JSON
// the contract names or throws an ApiError carrying the contract's error code. The screens decide by `code`, never by
// the message.
import type {
  AgentStatus,
  Config,
  Envelope,
  Invite,
  KeyCheck,
  Machine,
  MachineCode,
  Me,
  Member,
  OnboardingStep,
  Passkey,
  PreflightReport,
  Profile,
  Provider,
  ProviderCheck,
  ProxyScope,
  ProxySummary,
  Role,
  SignedIn,
  Space,
  SpaceRef,
  User,
  Login,
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
  const call = async <T>(method: string, path: string, body?: unknown, query?: Query): Promise<T> => {
    const init: RequestInit = { method, credentials: 'same-origin', headers: { Accept: 'application/json' } };
    if (method !== 'GET') {
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

  return {
    config: () => get<Config>('/api/config'),

    auth: {
      emailStart: (email: string, intent: 'signin' | 'signup') =>
        post<{ resendAfter: number; expiresIn: number }>('/api/auth/email/start', { email, intent }),
      emailVerify: (email: string, code: string) => post<SignedIn>('/api/auth/email/verify', { email, code }),
      passkeyBegin: () => post<{ challengeId: string; publicKey: unknown }>('/api/auth/passkey/login/begin'),
      passkeyFinish: (challengeId: string, credential: unknown) =>
        post<SignedIn>('/api/auth/passkey/login/finish', { challengeId, credential }),
      logout: () => post<void>('/api/auth/logout'),
    },

    me: {
      get: () => get<Me>('/api/me'),
      rename: (name: string) => patch<{ user: User }>('/api/me', { name }),
      passkeyBegin: () => post<{ challengeId: string; publicKey: unknown }>('/api/me/passkeys/begin'),
      passkeyFinish: (challengeId: string, credential: unknown, name: string) =>
        post<{ passkey: Passkey }>('/api/me/passkeys/finish', { challengeId, credential, name }),
      removePasskey: (id: string) => del(`/api/me/passkeys/${enc(id)}`),
      removeIdentity: (provider: string) => del(`/api/me/identities/${enc(provider)}`),
    },

    spaces: {
      create: (name: string) => post<{ space: Space }>('/api/spaces', { name }),
      get: (id: string) => get<{ space: Space }>(`/api/spaces/${enc(id)}`),
      bySlug: (slug: string) => get<{ space: Space }>(`/api/spaces/by-slug/${enc(slug)}`),
      update: (id: string, patchBody: { name?: string; onboardingStep?: OnboardingStep }) =>
        patch<{ space: Space }>(`/api/spaces/${enc(id)}`, patchBody),
      members: (id: string) => get<{ members: Member[] }>(`/api/spaces/${enc(id)}/members`),
      setRole: (id: string, userId: string, role: Role) =>
        patch<{ member: Member }>(`/api/spaces/${enc(id)}/members/${enc(userId)}`, { role }),
      removeMember: (id: string, userId: string) => del(`/api/spaces/${enc(id)}/members/${enc(userId)}`),
      invite: (id: string, emails: string[]) =>
        post<{ invites: Invite[] }>(`/api/spaces/${enc(id)}/invites`, { emails }),
      inviteLink: (id: string, opts: { maxUses?: number; days?: number } = {}) =>
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
      ssh: (
        spaceId: string,
        req: {
          host: string;
          port: number;
          user: string;
          profile: Profile;
          confirmFull: boolean;
          hostKeyFingerprint?: string;
        },
      ) =>
        post<{ jobId: string }>(`/api/spaces/${enc(spaceId)}/machines/ssh`, {
          host: req.host,
          port: req.port,
          user: req.user,
          profile: req.profile,
          ...(req.confirmFull ? { confirmFull: true } : {}),
          ...(req.hostKeyFingerprint ? { hostKeyFingerprint: req.hostKeyFingerprint } : {}),
        }),
      sshKey: (spaceId: string) => get<{ publicKey: string }>(`/api/spaces/${enc(spaceId)}/machines/ssh`),
      get: (id: string) => get<{ machine: Machine }>(`/api/machines/${enc(id)}`),
      rename: (id: string, name: string) => patch<{ machine: Machine }>(`/api/machines/${enc(id)}`, { name }),
      remove: (id: string) => del(`/api/machines/${enc(id)}`),
      open: (id: string) => get<{ url: string }>(`/api/machines/${enc(id)}/open`),
      preflight: (id: string) => post<{ preflight: PreflightReport }>(`/api/machines/${enc(id)}/preflight`),
    },

    agents: {
      list: (id: string, refresh = false) =>
        get<{ agents: AgentStatus[] }>(`/api/machines/${enc(id)}/agents`, {
          refresh: refresh ? 1 : undefined,
        }),
      install: (id: string, provider: Provider, from?: 'provider') =>
        post<{ jobId: string }>(`/api/machines/${enc(id)}/agents/${provider}/install`, from ? { from } : {}),
      check: (id: string, provider: Provider, proxyUrl?: string) =>
        post<{ check: ProviderCheck }>(
          `/api/machines/${enc(id)}/agents/${provider}/check`,
          proxyUrl ? { proxyUrl } : {},
        ),
      login: (id: string, provider: Provider, method: 'link' | 'console') =>
        post<{ login: Login }>(`/api/machines/${enc(id)}/agents/${provider}/login`, { method }),
      loginCode: (id: string, loginId: string, code: string) =>
        post<void>(`/api/machines/${enc(id)}/logins/${enc(loginId)}/code`, { code }),
      loginCancel: (id: string, loginId: string) => del(`/api/machines/${enc(id)}/logins/${enc(loginId)}`),
      deliverKey: (id: string, provider: Provider, envelope: Envelope) =>
        post<{ check: KeyCheck }>(`/api/machines/${enc(id)}/agents/${provider}/key`, { envelope }),
      verifyKey: (id: string, provider: Provider) =>
        post<{ check: KeyCheck }>(`/api/machines/${enc(id)}/agents/${provider}/key/verify`),
    },

    proxy: {
      get: (id: string) => get<{ proxy: ProxySummary }>(`/api/machines/${enc(id)}/proxy`),
      set: (id: string, scope: ProxyScope, url: string) =>
        put<{ proxy: ProxySummary }>(`/api/machines/${enc(id)}/proxy`, { scope, url }),
      clear: (id: string, scope: ProxyScope) =>
        del<{ proxy: ProxySummary }>(`/api/machines/${enc(id)}/proxy`, { scope }),
    },

    env: {
      deliver: (id: string, name: string, envelope: Envelope) =>
        post<{ env: string[] }>(`/api/machines/${enc(id)}/env`, { name, envelope }),
      remove: (id: string, name: string) =>
        del<{ env: string[] }>(`/api/machines/${enc(id)}/env/${enc(name)}`),
      gitKey: (id: string) => post<{ publicKey: string }>(`/api/machines/${enc(id)}/gitkey`),
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
export const eventsUrl = (spaceId: string): string => `/api/spaces/${enc(spaceId)}/events`;
