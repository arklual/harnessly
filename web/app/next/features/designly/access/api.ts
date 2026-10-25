// Designly access over the cluster's gateway (<base><platform api>/design/access/*), the same base the docs ask
// (`gatewayPath`). Standalone there is no gateway: the project's access then reads as `null` and the screens stay
// quiet. Every change waits for the gateway and answers with the project's fresh state.
import { gatewayPath, platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

export const ROLE = {
  owner: 'owner',
  manage: 'manage',
  edit: 'edit',
  comment: 'comment',
  view: 'view',
} as const;
export type TRole = (typeof ROLE)[keyof typeof ROLE];
export type TGrantRole = Exclude<TRole, 'owner'>;
export type TInviteRole = 'view' | 'comment' | 'edit';
export type TTeamLevel = 'none' | 'view' | 'comment' | 'edit';
export type TRequestLevel = 'comment' | 'edit';

export const TTL = { hour: '1h', day: '1d', week: '7d', month: '30d' } as const;
/** What the gateway takes for a term: a step, an ISO moment, or null for ever. */
export type TTtlValue = (typeof TTL)[keyof typeof TTL] | string | null;

export interface IPerson {
  email: string;
  role: TGrantRole;
  grantedBy: string;
  grantedAt: string;
  expiresAt: string | null;
  viaInvite: string | null;
}

export interface IInvite {
  id: string;
  title: string;
  role: TInviteRole;
  roleTtl: string | null;
  expiresAt: string;
  maxUses: number | null;
  uses: number;
  revokedAt: string | null;
}

export interface IAccessRequest {
  id: string;
  email: string;
  level: TRequestLevel;
  message: string;
  status: 'open' | 'allowed' | 'denied';
  createdAt: string;
}

/** `GET /:project`; the last three only come to a person who manages. */
export interface IProjectAccess {
  role: TRole | null;
  /** Empty for a stranger: they get the name to ask by, not the address. */
  owner: string | null;
  ownerName: string;
  team: TTeamLevel;
  canManage: boolean;
  editorsCanShare: boolean;
  request: IAccessRequest | null;
  people?: IPerson[];
  invites?: IInvite[];
  requests?: IAccessRequest[];
}

export interface ILogEntry {
  id: number;
  actor: string;
  action: string;
  subject: string;
  at: string;
}

export interface IInviteMade {
  id: string;
  token: string;
  url: string;
  expiresAt: string;
}

export type TGuestRights = 'view' | 'comment';

/** A link that opens the design without signing in (`…/shares`); `comments` is how many guests wrote under it. */
export interface IGuestLink {
  id: string;
  title: string;
  rights: TGuestRights;
  expiresAt: string | null;
  revokedAt: string | null;
  opens: number;
  createdAt: string;
  url: string;
  comments: number;
}

/** How the owner looks at the design, for a guest link to open the same way (the contract is in access/guest-view.ts). */
export interface IGuestView {
  v: 1;
  device: string;
  rotated: boolean;
  width: number | null;
  height: number | null;
  chrome: boolean;
  browser: string | null;
  fit: string | null;
  zoom: number | null;
  panX: number | null;
  panY: number | null;
  locked: boolean;
  page: string | null;
}

export interface IGuestLinkInput {
  title: string;
  rights: TGuestRights;
  ttl: TTtlValue;
  /** The owner's remembered look; absent when the design has none. */
  view?: IGuestView;
}

export interface IPersonHit {
  id: string;
  name: string;
}

/** The gateway's refusal in its own words, with its code (`forbidden`, `gone`, `conflict`, `rate_limited`...). */
export class AccessError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly status: number,
  ) {
    super(message);
  }
}

export const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

const TIMEOUT_MS = 15_000;
const BASE = '/design/access';

const send = async (
  method: string,
  path: `/${string}`,
  body?: unknown,
  signal?: AbortSignal,
): Promise<unknown> => {
  const ctl = new AbortController();
  signal?.addEventListener('abort', () => ctl.abort());
  const timer = setTimeout(() => ctl.abort(), TIMEOUT_MS);
  let res: Response;
  try {
    res = await fetch(gatewayPath(path), {
      method,
      credentials: 'same-origin',
      cache: 'no-store',
      signal: ctl.signal,
      ...(body === undefined
        ? {}
        : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
    });
  } catch (failure) {
    if (signal?.aborted) throw failure;
    throw new AccessError(
      ctl.signal.aborted ? t('next.access.failure.timeout') : t('next.access.failure.network'),
      'network',
      0,
    );
  } finally {
    clearTimeout(timer);
  }
  const answer: unknown = await res.json().catch(() => null);
  if (res.ok) return answer;
  const said =
    typeof answer === 'object' && answer !== null ? (answer as { error?: unknown; code?: unknown }) : {};
  throw new AccessError(
    typeof said.error === 'string' && said.error ? said.error : t('next.failure.http'),
    typeof said.code === 'string' ? said.code : 'unknown',
    res.status,
  );
};

const at = (project: string) => `${BASE}/${encodeURIComponent(project)}` as const;

const isAccess = (v: unknown): v is IProjectAccess =>
  typeof v === 'object' && v !== null && 'team' in v && 'canManage' in v;

/** The project's access, or null where there is no gateway (standalone, an old gateway, no answer): nothing is shown then. */
export const fetchAccess = async (project: string, signal?: AbortSignal): Promise<IProjectAccess | null> => {
  try {
    const answer = await send('GET', at(project), undefined, signal);
    return isAccess(answer) ? answer : null;
  } catch (failure) {
    if (signal?.aborted) throw failure;
    return null;
  }
};

const fresh = async (call: Promise<unknown>): Promise<IProjectAccess> => {
  const answer = await call;
  if (!isAccess(answer)) throw new AccessError(t('next.failure.http'), 'unknown', 0);
  return answer;
};

export const putTeam = (project: string, level: TTeamLevel) =>
  fresh(send('PUT', `${at(project)}/team`, { level }));

export const putPerson = (project: string, email: string, role: TGrantRole, ttl: TTtlValue) =>
  fresh(send('PUT', `${at(project)}/people/${encodeURIComponent(email)}`, { role, ttl }));

export const removePerson = (project: string, email: string) =>
  fresh(send('DELETE', `${at(project)}/people/${encodeURIComponent(email)}`));

/** A request for a role; the answer is the project's state with the request in it. */
export const askAccess = (project: string, level: TRequestLevel, message: string) =>
  fresh(send('POST', `${at(project)}/requests`, { level, message }));

export const decideRequest = (
  project: string,
  id: string,
  decision: { decision: 'deny' } | { decision: 'allow'; role: TGrantRole; ttl: TTtlValue },
) => fresh(send('POST', `${at(project)}/requests/${encodeURIComponent(id)}`, decision));

export interface IInviteInput {
  title: string;
  role: TInviteRole;
  roleTtl: TTtlValue;
  ttl: TTtlValue;
  maxUses: number | null;
}

export const createInvite = async (project: string, input: IInviteInput): Promise<IInviteMade> => {
  const answer = (await send('POST', `${at(project)}/invites`, input)) as IInviteMade;
  if (typeof answer?.url !== 'string') throw new AccessError(t('next.failure.http'), 'unknown', 0);
  return answer;
};

export const revokeInvite = (project: string, id: string) =>
  fresh(send('DELETE', `${at(project)}/invites/${encodeURIComponent(id)}`));

const isGuestLink = (v: unknown): v is IGuestLink =>
  typeof v === 'object' &&
  v !== null &&
  'id' in v &&
  'rights' in v &&
  typeof (v as IGuestLink).url === 'string';

const guestLink = (answer: unknown): IGuestLink => {
  if (!isGuestLink(answer)) throw new AccessError(t('next.failure.http'), 'unknown', 0);
  return answer;
};

/** The links without sign-in, or null where the gateway has none (an old one, no answer): the block stays away then. */
export const fetchGuestLinks = async (
  project: string,
  signal?: AbortSignal,
): Promise<IGuestLink[] | null> => {
  try {
    const answer = await send('GET', `${at(project)}/shares`, undefined, signal);
    return Array.isArray(answer) ? answer.filter(isGuestLink) : null;
  } catch (failure) {
    if (signal?.aborted) throw failure;
    return null;
  }
};

export const createGuestLink = async (project: string, input: IGuestLinkInput): Promise<IGuestLink> =>
  guestLink(await send('POST', `${at(project)}/shares`, input));

/** A change of a link keeps its address; the answer is the link as it is now. */
export const changeGuestLink = async (
  project: string,
  id: string,
  change: Partial<IGuestLinkInput>,
): Promise<IGuestLink> =>
  guestLink(await send('PATCH', `${at(project)}/shares/${encodeURIComponent(id)}`, change));

export const revokeGuestLink = async (project: string, id: string): Promise<void> => {
  await send('DELETE', `${at(project)}/shares/${encodeURIComponent(id)}`);
};

/** Takes a link: the project it opens. 410 is a link that ran out, was revoked or is spent. */
export const acceptInvite = async (
  token: string,
): Promise<{ project: string; role: TRole; until: string | null }> =>
  (await send('POST', `${BASE}/invites/${encodeURIComponent(token)}/accept`, {})) as {
    project: string;
    role: TRole;
    until: string | null;
  };

export const revokeAll = (project: string) => fresh(send('POST', `${at(project)}/revoke-all`, {}));

export const transferOwner = (project: string, email: string) =>
  fresh(send('POST', `${at(project)}/owner`, { email }));

export const setEditorsCanShare = (project: string, value: boolean) =>
  fresh(send('PUT', `${at(project)}/settings`, { editorsCanShare: value }));

export const fetchLog = async (project: string, signal?: AbortSignal): Promise<ILogEntry[]> => {
  const answer = (await send('GET', `${at(project)}/log`, undefined, signal)) as { log?: ILogEntry[] };
  return Array.isArray(answer?.log) ? answer.log : [];
};

/** People by name or address, the search the documents use for «@». */
export const searchPeople = async (query: string, signal?: AbortSignal): Promise<IPersonHit[]> => {
  const answer = (await send('GET', `/docs/people?q=${encodeURIComponent(query)}`, undefined, signal)) as {
    people?: IPersonHit[];
  };
  return Array.isArray(answer?.people) ? answer.people : [];
};

/** The runtime takes a project nobody owns for the person (404: not here, 409: it has an owner). */
export const claimOwnership = (machine: MachineId, project: string) =>
  runtimeJson<unknown>(machine, platformPath('/design/library/claim'), postJson({ project }));

/** True when the person holds this role or a stronger one. */
const ORDER: readonly TRole[] = ['view', 'comment', 'edit', 'manage', 'owner'];
export const atLeast = (role: TRole | null, need: TRole): boolean =>
  role !== null && ORDER.indexOf(role) >= ORDER.indexOf(need);

/** Nobody owns the project: the gateway names no owner and no name to ask by. */
export const isUnowned = (a: IProjectAccess): boolean => !a.owner && !a.ownerName;

/** The term as the gateway takes it: «до даты» is the end of that day. */
export const endOfDay = (date: string): string => {
  const [y, m, d] = date.split('-').map(Number);
  return new Date(y ?? 1970, (m ?? 1) - 1, d ?? 1, 23, 59, 59).toISOString();
};
