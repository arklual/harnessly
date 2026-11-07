// The transport of the live org API: the hub's JSON and error body (docs/hub/api.md, orgs.md) turned into OrgApiError.
import type { Needs } from '../../../../hub-api/orgs';
import { noteCurrency } from '../../money';
import { OrgApiError } from '../errors';

const CODES: Record<string, OrgApiError['code']> = {
  forbidden: 'forbidden',
  forbidden_scope: 'forbidden',
  self_change: 'self_change',
  last_admin: 'last_admin',
  not_found: 'not_found',
  invalid_request: 'invalid_request',
  no_recipient: 'no_recipient',
  restart_unsupported: 'restart_unsupported',
  exists: 'exists',
  conflict: 'hub',
  locked: 'locked',
  name_taken: 'name_taken',
  built_in: 'built_in',
  not_enabled: 'not_enabled',
  no_machine: 'no_machine',
  busy: 'busy',
  machine_untrusted: 'untrusted',
};

export type Query = Record<string, string | number | undefined>;

const withQuery = (path: string, query?: Query): string => {
  const pairs = Object.entries(query ?? {}).filter((e): e is [string, string | number] => e[1] !== undefined);
  return pairs.length === 0
    ? path
    : `${path}?${pairs.map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`).join('&')}`;
};

const failureOf = async (res: Response): Promise<OrgApiError> => {
  type Body = { error?: unknown; message?: unknown; details?: { needs?: unknown } };
  const body = await res.json().then(
    (v: unknown) => (v && typeof v === 'object' ? (v as Body) : null),
    () => null,
  );
  const code = typeof body?.error === 'string' ? CODES[body.error] : undefined;
  const message = typeof body?.message === 'string' ? body.message : '';
  const needs = typeof body?.details?.needs === 'string' ? (body.details.needs as Needs) : null;
  return new OrgApiError(code ?? 'internal', message, needs);
};

const call = async <T>(method: string, path: string, body?: unknown, query?: Query): Promise<T> => {
  const headers: Record<string, string> = { Accept: 'application/json' };
  const init: RequestInit = { method, credentials: 'same-origin', headers };
  if (method !== 'GET') {
    // The hub refuses a change that is not JSON (cross_site), also one with no body.
    headers['Content-Type'] = 'application/json';
    if (body !== undefined) init.body = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await fetch(withQuery(path, query), init);
  } catch {
    throw new OrgApiError('network', '');
  }
  if (!res.ok) throw await failureOf(res);
  if (res.status === 204) return undefined as T;
  const out = (await res.json()) as T;
  noteCurrency(out);
  return out;
};

export const http = {
  get: <T>(path: string, query?: Query) => call<T>('GET', path, undefined, query),
  post: <T>(path: string, body: unknown = {}) => call<T>('POST', path, body),
  put: <T>(path: string, body: unknown = {}) => call<T>('PUT', path, body),
  patch: <T>(path: string, body: unknown = {}) => call<T>('PATCH', path, body),
  del: <T = undefined>(path: string, query?: Query) => call<T>('DELETE', path, undefined, query),
};

/** An endpoint of the contract the hub may not serve yet: its 404 says the data is not connected, not «такого нет». */
export const contractGet = async <T>(path: string, query?: Query): Promise<T> => {
  try {
    return await http.get<T>(path, query);
  } catch (e) {
    if (e instanceof OrgApiError && e.code === 'not_found') {
      throw new OrgApiError('not_ready', `${path} is not served yet`);
    }
    throw e;
  }
};

export const orgPath = (org: string, rest = ''): string => `/api/orgs/${encodeURIComponent(org)}${rest}`;
