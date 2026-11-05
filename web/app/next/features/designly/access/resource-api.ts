// The one access API of every kind of resource: <gateway>/access/{kind}/{id}, what the one «Поделиться»
// answers to. The shapes are the gateway's (its access types). A gateway that does not know the route
// yet answers nothing usable: `fetchView` says null and the caller keeps the older dialog. Every change waits for the
// gateway and answers with the resource's fresh view.
import { AccessError, send, type ILogEntry, type TTtlValue } from '@/next/features/designly/access/api';
import { t } from '@/next/i18n';

export type TShareRole = 'view' | 'comment' | 'edit';
export type TResourceKind = 'design' | 'doc';

export interface IGrant {
  type: 'user' | 'team';
  /** The address of a person, the id of a team. */
  id: string;
  name: string;
  /** The address of a person; «Организация · N» of a team. */
  sub: string;
  role: TShareRole;
  canShare: boolean;
  expiresAt: string | null;
}

export interface IResourceRequest {
  id: string;
  email: string;
  name: string;
  level: 'comment' | 'edit';
  message: string;
  createdAt: string;
}

/** A link without sign-in (stage «г»): its own for each recipient, view or comment, never edit. */
export interface IAccessLink {
  id: string;
  forWhom: string;
  rights: 'view' | 'comment';
  expiresAt: string | null;
  /** When it was switched off; a link whose maker may no longer share counts as off from now. */
  disabledAt: string | null;
  opens: number;
  lastOpenedAt: string | null;
  createdAt: string;
  createdBy: string;
  comments: number;
  /** What the link allows: a design's `view` (device, deck opening, pdf, video...), a document's `download` and `copy`. */
  options: Record<string, TLinkScalar>;
  version: 'latest' | 'pinned';
  pages: string[] | null;
  /** The address can be copied again; false for a link made before the addresses were kept (it can be reissued). */
  copyable: boolean;
}

export type TLinkScalar = string | number | boolean | null;

export interface IResourceView {
  resource: { kind: string; id: string; subkind: string; title: string };
  /** False while the resource has no row: nothing was changed yet, the older dialog handles it. */
  stored: boolean;
  role: 'owner' | TShareRole | null;
  until: string | null;
  canManage: boolean;
  /** Runs the access fully: the team's access, «may share», those who may. */
  manager: boolean;
  /** The home team's lead or its organization's admin. */
  steward: boolean;
  owner: { email: string | null; name: string } | null;
  homeTeam: { teamId: string; name: string; role: TShareRole | null } | null;
  editorsCanShare: boolean;
  grants?: IGrant[];
  requests?: IResourceRequest[];
  /** Links without sign-in, for those who run the access; absent from a gateway before stage «г». */
  links?: IAccessLink[];
  myRequest: { id: string; level: 'comment' | 'edit'; status: 'open' | 'allowed' | 'denied' } | null;
}

export interface IDirectory {
  people: { email: string; name: string; teams: string[] }[];
  teams: { teamId: string; name: string; orgName: string; members: number }[];
}

export interface IResourceRef {
  kind: TResourceKind;
  id: string;
}

const at = ({ kind, id }: IResourceRef) => `/access/${kind}/${encodeURIComponent(id)}` as `/${string}`;

export const isView = (v: unknown): v is IResourceView =>
  typeof v === 'object' && v !== null && 'resource' in v && 'canManage' in v && 'manager' in v;

/** The view, or null where the gateway does not answer this API (an older one): the caller keeps the older dialog. */
export const fetchView = async (ref: IResourceRef, signal?: AbortSignal): Promise<IResourceView | null> => {
  try {
    const answer = await send('GET', at(ref), undefined, signal);
    return isView(answer) ? answer : null;
  } catch (failure) {
    if (signal?.aborted) throw failure;
    return null;
  }
};

const fresh = async (call: Promise<unknown>): Promise<IResourceView> => {
  const answer = await call;
  if (!isView(answer)) throw new AccessError(t('next.failure.http'), 'unknown', 0);
  return answer;
};

export const putTeamRole = (ref: IResourceRef, role: TShareRole | null) =>
  fresh(send('PUT', `${at(ref)}/team`, { role }));

export interface IGrantInput {
  role: TShareRole;
  canShare: boolean;
  ttl: TTtlValue;
}

export const putGrant = (ref: IResourceRef, type: IGrant['type'], subject: string, input: IGrantInput) =>
  fresh(send('PUT', `${at(ref)}/grants/${type}/${encodeURIComponent(subject)}`, input));

export const removeGrant = (ref: IResourceRef, type: IGrant['type'], subject: string) =>
  fresh(send('DELETE', `${at(ref)}/grants/${type}/${encodeURIComponent(subject)}`));

export const putEditorsCanShare = (ref: IResourceRef, value: boolean) =>
  fresh(send('PUT', `${at(ref)}/settings`, { editorsCanShare: value }));

export const handOver = (ref: IResourceRef, email: string) =>
  fresh(send('POST', `${at(ref)}/owner`, { email }));

export const askFor = (ref: IResourceRef, level: 'comment' | 'edit', message = '') =>
  fresh(send('POST', `${at(ref)}/requests`, { level, message }));

export const decide = (
  ref: IResourceRef,
  id: string,
  decision: { decision: 'allow'; role: TShareRole; canShare: boolean; ttl: TTtlValue } | { decision: 'deny' },
) => fresh(send('POST', `${at(ref)}/requests/${encodeURIComponent(id)}`, decision));

export const fetchResourceLog = async (ref: IResourceRef, signal?: AbortSignal): Promise<ILogEntry[]> => {
  const answer = (await send('GET', `${at(ref)}/log`, undefined, signal)) as { log?: ILogEntry[] };
  return Array.isArray(answer?.log) ? answer.log : [];
};

const isDirectory = (v: unknown): v is IDirectory =>
  typeof v === 'object' &&
  v !== null &&
  Array.isArray((v as IDirectory).people) &&
  Array.isArray((v as IDirectory).teams);

/** People and teams to add; an empty list for someone outside every team (the gateway decides). */
export const searchDirectory = async (q: string, signal?: AbortSignal): Promise<IDirectory> => {
  const answer = await send('GET', `/access/directory?q=${encodeURIComponent(q)}`, undefined, signal);
  return isDirectory(answer) ? answer : { people: [], teams: [] };
};

export interface ILinkInput {
  forWhom: string;
  rights: 'view' | 'comment';
  ttl: TTtlValue;
  options: Record<string, TLinkScalar> | null;
  version: 'latest' | 'pinned';
  pages: string[] | null;
}

export interface ILinkChange extends Partial<ILinkInput> {
  disabled?: boolean;
}

const isLink = (v: unknown): v is IAccessLink =>
  typeof v === 'object' && v !== null && typeof (v as IAccessLink).id === 'string' && 'forWhom' in v;

const linkOf = (answer: unknown): IAccessLink => {
  const link = (answer as { link?: unknown } | null)?.link;
  if (!isLink(link)) throw new AccessError(t('next.failure.http'), 'unknown', 0);
  return link;
};

const urlOf = (answer: unknown): string | null => {
  const url = (answer as { url?: unknown } | null)?.url;
  return typeof url === 'string' ? url : null;
};

/** Makes a link; the answer has its address, the one moment it is surely known. */
export const createLink = async (
  ref: IResourceRef,
  input: ILinkInput,
): Promise<{ link: IAccessLink; url: string }> => {
  const answer = await send('POST', `${at(ref)}/links`, input);
  const url = urlOf(answer);
  if (url === null) throw new AccessError(t('next.failure.http'), 'unknown', 0);
  return { link: linkOf(answer), url };
};

/** Changes a link; `disabled: true` switches it off at once, `false` on again. */
export const changeLink = async (ref: IResourceRef, id: string, change: ILinkChange): Promise<IAccessLink> =>
  linkOf(await send('PATCH', `${at(ref)}/links/${encodeURIComponent(id)}`, change));

/** The address again; null for a link made before the addresses were kept. */
export const linkAddress = async (ref: IResourceRef, id: string): Promise<string | null> =>
  urlOf(await send('GET', `${at(ref)}/links/${encodeURIComponent(id)}/address`));

/** A new address; the old one stops opening at once. */
export const reissueLink = async (ref: IResourceRef, id: string): Promise<string> => {
  const url = urlOf(await send('POST', `${at(ref)}/links/${encodeURIComponent(id)}/address`));
  if (url === null) throw new AccessError(t('next.failure.http'), 'unknown', 0);
  return url;
};
