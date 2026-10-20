import { apiUrl } from './base';

export class HttpError extends Error {
  constructor(
    readonly status: number,
    readonly body: unknown,
    message: string,
  ) {
    super(message);
    this.name = 'HttpError';
  }
}

type Listener = () => void;
const unauthorized = new Set<Listener>();

/** Called whenever the runtime refuses the session, so the page can go back to sign-in. */
export function onUnauthorized(fn: Listener): () => void {
  unauthorized.add(fn);
  return () => unauthorized.delete(fn);
}

function errorText(body: unknown, status: number): string {
  if (body && typeof body === 'object' && 'error' in body && typeof body.error === 'string')
    return body.error;
  return `Request failed (${status})`;
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
  /** Leave a 401 to the caller instead of signalling a lost session. */
  quietAuth?: boolean;
}

/** A JSON call to the runtime's API with the page's session cookie. */
export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const res = await fetch(apiUrl(path), {
    method: opts.method ?? (opts.body === undefined ? 'GET' : 'POST'),
    credentials: 'same-origin',
    headers:
      opts.body === undefined ? { Accept: 'application/json' } : { 'Content-Type': 'application/json' },
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
    signal: opts.signal,
  });
  const text = await res.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = text;
    }
  }
  if (!res.ok) {
    if (res.status === 401 && !opts.quietAuth) unauthorized.forEach((fn) => fn());
    throw new HttpError(res.status, body, errorText(body, res.status));
  }
  return body as T;
}
