// The one resolver of where a machine's API and socket are (spec §5). Standalone the runtime answers both kinds of
// path and ignores machine=local; in a cluster the gateway answers the cluster paths itself and proxies the rest only
// with machine — a machine is never picked implicitly.

import type { MachineId } from '@/next/protocol/v2';
import { platformApi } from '@/platform/config';

type Path = `/${string}`;

/** The prefix the runtime put in <meta name="api-base"> (e.g. /agents), without the trailing slash. */
function apiBase(): string {
  if (typeof document === 'undefined') return '';
  const v = document.querySelector('meta[name="api-base"]')?.getAttribute('content') ?? '';
  return v.replace(/\/+$/, '');
}

let base: () => string = apiBase;
let origin: () => { protocol: string; host: string } = () =>
  typeof location === 'undefined' ? { protocol: 'http:', host: 'localhost' } : location;

/** Points the resolver elsewhere (tests, a fake runtime). */
export function setApiOrigin(next: { base?: string; protocol?: string; host?: string }): void {
  if (next.base !== undefined) base = () => next.base as string;
  if (next.host !== undefined) {
    const o = { protocol: next.protocol ?? 'http:', host: next.host };
    origin = () => o;
  }
}

const withMachine = (path: string, m: MachineId) =>
  `${path}${path.includes('?') ? '&' : '?'}machine=${encodeURIComponent(m)}`;

export const machineApi = (m: MachineId) => ({
  http: (path: Path) => `${base()}/api/v2${withMachine(path, m)}`,
  ws: (path: Path = '/socket') => {
    const o = origin();
    return `${o.protocol === 'https:' ? 'wss:' : 'ws:'}//${o.host}${base()}${withMachine(`/api/v2${path}`, m)}`;
  },
});

/** A path of the runtime outside v2 (the design library, the preview) on one machine: Designly runs on them until the
 *  library has a v2 surface. */
export const runtimePath = (path: Path, m: MachineId) => `${base()}${withMachine(path, m)}`;

/** A socket of the runtime outside v2 (dictation, /api/voice) on one machine: the same path as `runtimePath`, on ws(s). */
export const runtimeSocket = (path: Path, m: MachineId) => {
  const o = origin();
  return `${o.protocol === 'https:' ? 'wss:' : 'ws:'}//${o.host}${runtimePath(path, m)}`;
};

/** A path of the runtime's platform API: the runtime serves it under its own prefix (RUNTIME_PLATFORM_API, e.g.
 *  /api/acme), the default /api/platform. */
export const platformPath = (path: Path): Path => `${platformApi()}${path}` as Path;

/** A route of the cluster's gateway (docs, design access), under the base and the platform's prefix the runtime
 *  serves too (/agents/api/acme/...). Standalone there is none: the runtime answers such a path with 404, and the
 *  screens read that as «not here». */
export const gatewayPath = (path: Path): Path => `${base()}${platformApi()}${path}` as Path;

/** me, machines, search, vault, push: the cluster's (standalone: the runtime's) paths, no machine. */
export const clusterApi = (path: Path) => `${base()}/api/v2${path}`;
