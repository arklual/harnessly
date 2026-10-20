/** The prefix every API call and socket of the page takes: what the runtime put in <meta name="api-base">. */
export function apiBase(): string {
  if (typeof document === 'undefined') return '';
  const v = document.querySelector('meta[name="api-base"]')?.getAttribute('content') ?? '';
  return v.replace(/\/+$/, '');
}

export function apiUrl(path: string): string {
  return apiBase() + path;
}

/** The socket URL of an API path on this page's host. */
export function socketUrl(path: string, loc: Pick<Location, 'protocol' | 'host'> = window.location): string {
  const scheme = loc.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${scheme}//${loc.host}${apiUrl(path)}`;
}

/** The prefix of the platform's own routes (RUNTIME_PLATFORM_API on the runtime). */
export function platformApi(): string {
  if (typeof document === 'undefined') return '/api/platform';
  const v = document.querySelector('meta[name="platform-api"]')?.getAttribute('content') || '/api/platform';
  return v.replace(/\/+$/, '');
}
