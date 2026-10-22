import { addressMachineId, currentMachineId } from './machine';

/** The prefix every API call and socket of the page takes: what the runtime put in <meta name="api-base">. */
export function apiBase(): string {
  if (typeof document === 'undefined') return '';
  const v = document.querySelector('meta[name="api-base"]')?.getAttribute('content') ?? '';
  return v.replace(/\/+$/, '');
}

/**
 * A page opened for one machine of a fleet (`?machine=` in its address, as a machine's frame is), or drawing one
 * machine's conversation without a frame, keeps every call on that machine: a front that routes by the parameter
 * would otherwise send them to the default one. A call that names its machine keeps it.
 */
function withMachine(path: string, machine: string | undefined): string {
  if (!machine) return path;
  const hashAt = path.indexOf('#');
  const hash = hashAt < 0 ? '' : path.slice(hashAt);
  const rest = hashAt < 0 ? path : path.slice(0, hashAt);
  const queryAt = rest.indexOf('?');
  const params = new URLSearchParams(queryAt < 0 ? '' : rest.slice(queryAt + 1));
  if (params.has('machine')) return path;
  params.set('machine', machine);
  return `${queryAt < 0 ? rest : rest.slice(0, queryAt)}?${params}${hash}`;
}

export function apiUrl(path: string): string {
  return apiBase() + withMachine(path, currentMachineId());
}

const socketOf = (url: string, loc: Pick<Location, 'protocol' | 'host'>) =>
  `${loc.protocol === 'https:' ? 'wss:' : 'ws:'}//${loc.host}${url}`;

/** The socket URL of an API path on this page's host. */
export function socketUrl(path: string, loc: Pick<Location, 'protocol' | 'host'> = window.location): string {
  return socketOf(apiUrl(path), loc);
}

/**
 * The socket of the page's own machine (its address's, the home one without), whichever conversation is drawn:
 * for a socket that lives across conversations and feeds the shell's own stores (statuses, sync).
 */
export function homeSocketUrl(
  path: string,
  loc: Pick<Location, 'protocol' | 'host'> = window.location,
): string {
  return socketOf(apiBase() + withMachine(path, addressMachineId()), loc);
}

/** The prefix of the platform's own routes (RUNTIME_PLATFORM_API on the runtime). */
export function platformApi(): string {
  if (typeof document === 'undefined') return '/api/platform';
  const v = document.querySelector('meta[name="platform-api"]')?.getAttribute('content') || '/api/platform';
  return v.replace(/\/+$/, '');
}
