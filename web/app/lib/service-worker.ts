import { prefixAppPath } from '@/lib/app-path';

/** The worker controls the agent app only, never the Open WebUI paths beside it. */
export const SERVICE_WORKER_SCOPE = prefixAppPath('/');

/**
 * A production page pins the worker to its Next build, which turns on the app shell
 * cache in public/sw.js (shared by all builds, entries stamped per build). Without a build id the
 * worker only handles web push.
 */
export const serviceWorkerScriptUrl = (buildId: string | null): string =>
  prefixAppPath(buildId ? `/sw.js?v=${encodeURIComponent(buildId)}` : '/sw.js');
