import { appFetch, prefixAppPath } from '@/lib/app-path';
import type {
  IDocAccess,
  IDocDetailResponse,
  IDocDiffResponse,
  IDocListResponse,
  IDocPeopleResponse,
  IDocThread,
  IDocVersionsResponse,
  TDocThreadStatus,
} from '@/types/doc-threads';

const BASE = '/api/platform/docs';

class DocThreadsError extends Error {}

/** The gateway says what went wrong in `{error}`; that text is shown as it is, the status line only when there is none. */
const asJson = async <T>(response: Response): Promise<T> => {
  if (!response.ok) {
    const said = await response
      .json()
      .then((body: { error?: unknown }) => (typeof body?.error === 'string' ? body.error : ''))
      .catch(() => '');
    throw new DocThreadsError(said || `${response.status} ${response.statusText}`);
  }
  return response.json() as Promise<T>;
};

export const fetchDocList = async (): Promise<IDocListResponse> => asJson(await appFetch(BASE));

export const fetchDocDetail = async (slug: string): Promise<IDocDetailResponse> =>
  asJson(await appFetch(`${BASE}/${encodeURIComponent(slug)}`));

export const createDocThread = async (
  slug: string,
  anchor: IDocThread['anchor'],
  body: string,
): Promise<{ thread: IDocThread }> =>
  asJson(
    await appFetch(`${BASE}/${encodeURIComponent(slug)}/threads`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ anchor, body }),
    }),
  );

export const replyToDocThread = async (
  slug: string,
  threadId: string,
  body: string,
): Promise<{ thread: IDocThread }> =>
  asJson(
    await appFetch(`${BASE}/${encodeURIComponent(slug)}/threads/${encodeURIComponent(threadId)}/comments`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ body }),
    }),
  );

export const setDocThreadStatus = async (
  slug: string,
  threadId: string,
  status: TDocThreadStatus,
): Promise<{ thread: IDocThread }> =>
  asJson(
    await appFetch(`${BASE}/${encodeURIComponent(slug)}/threads/${encodeURIComponent(threadId)}/status`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ status }),
    }),
  );

export const markDocRead = async (slug: string): Promise<void> => {
  await appFetch(`${BASE}/${encodeURIComponent(slug)}/read`, { method: 'POST' });
};

/** The page of one version when `version` is given (its id), else the current one. */
export const docContentSrc = (slug: string, version: string | null = null): string =>
  prefixAppPath(
    `${BASE}/${encodeURIComponent(slug)}/content${version ? `?v=${encodeURIComponent(version)}` : ''}`,
  );

export const fetchDocVersions = async (slug: string): Promise<IDocVersionsResponse> =>
  asJson(await appFetch(`${BASE}/${encodeURIComponent(slug)}/versions`));

export const fetchDocDiff = async (slug: string, from: string, to: string): Promise<IDocDiffResponse> =>
  asJson(
    await appFetch(
      `${BASE}/${encodeURIComponent(slug)}/diff?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    ),
  );

/** People for the @ list; `q` is what was typed after the `@`. */
export const fetchDocPeople = async (q: string): Promise<IDocPeopleResponse> =>
  asJson(await appFetch(`${BASE}/people?q=${encodeURIComponent(q)}`));

export const fetchDocAccess = async (slug: string): Promise<IDocAccess> =>
  asJson(await appFetch(`${BASE}/${encodeURIComponent(slug)}/access`));

/** Opens (true) or closes (false) the public link; closing revokes the old address for good. */
export const setDocPublic = async (slug: string, open: boolean): Promise<IDocAccess> =>
  asJson(
    await appFetch(`${BASE}/${encodeURIComponent(slug)}/access`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ public: open }),
    }),
  );
