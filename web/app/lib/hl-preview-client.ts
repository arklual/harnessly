import { t } from '@/lib/i18n';
import { appHost } from '@/lib/site';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import type { IPickedComponent } from '@/lib/hl-preview/bridge-core';
import type { IPreviewPort } from '@/lib/hl-preview/ports-shared';
import type { IResolvedMatch } from '@/types/server/resolve';

/** Only the fleet gateway issues slots; a runtime opened directly answers 404. */
export class PreviewUnavailableError extends Error {
  constructor() {
    super(t('lib.hlPreviewClient.gatewayOnly'));
  }
}

export interface IOpenedPreview {
  slot: number;
  origin: string;
  enterUrl: string;
  /** Neighbour ports (MFE remotes) served under `/__tk/p/<port>` of this slot: a reload opens them again. */
  neighbours: number[];
  /** The machine the preview was opened on when it is not the page's own (a board of another machine's library). */
  machineId?: string;
}

const PORT_INPUT = /^(?:(?:https?:\/\/)?(?:localhost|127\.0\.0\.1|0\.0\.0\.0)?:)?(\d{1,5})(\/.*)?$/i;

export interface IPreviewTarget {
  port: number;
  path: string;
}

export interface IPortTarget extends IPreviewTarget {
  kind: 'port';
}

export interface IUrlTarget {
  kind: 'url';
  /** `https://host[:port]`, as `URL.origin` writes it. */
  origin: string;
  path: string;
}

/** What the preview opens: a dev server of the machine or an external site. */
export type TPreviewTarget = IPortTarget | IUrlTarget;

/** The slot lease key of a target, without the path. */
export type TSlotTarget = { kind: 'port'; port: number } | { kind: 'url'; origin: string };

export const PATH_ERROR = t('lib.hlPreviewClient.invalidPath');

/** The path leaves as ASCII: the gateway refuses anything else, and an unencoded Cyrillic Location would be invalid. */
export const encodePath = (path: string): string => {
  try {
    return encodeURI(decodeURI(path));
  } catch {
    throw new Error(PATH_ERROR);
  }
};

/** Throws PATH_ERROR for a path with broken percent-encoding. */
export const parsePortInput = (value: string): IPreviewTarget | null => {
  const match = PORT_INPUT.exec(value.trim());
  if (!match) return null;
  const port = Number(match[1]);
  return port >= 1 && port <= 65535 ? { port, path: encodePath(match[2] ?? '/') } : null;
};

/** Typed text that is an address and not a port: `example.com`, `https://admin.example.com/events`. */
const SCHEME = /^[a-z][a-z0-9+.-]*:\/\//i;

export const urlTarget = (url: URL): IUrlTarget => ({
  kind: 'url',
  origin: url.origin,
  path: `${url.pathname}${url.search}`,
});

/**
 * A port or an address. Without a scheme https is assumed; only http(s) without credentials and with a dotted host (or an
 * IP) is an address. Throws PATH_ERROR like `parsePortInput`; null when it is neither.
 */
export const parsePreviewTarget = (value: string): TPreviewTarget | null => {
  const port = parsePortInput(value);
  if (port) return { kind: 'port', ...port };
  const text = value.trim();
  // Digits alone are a mistyped port, not the decimal form of an IP.
  if (!text || /\s/.test(text) || /^\d+$/.test(text)) return null;
  let url: URL;
  try {
    url = new URL(SCHEME.test(text) ? text : `https://${text}`);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return null;
  if (url.username || url.password) return null;
  if (!url.hostname.includes('.') && !url.hostname.includes(':')) return null;
  return urlTarget(url);
};

/** What the window header shows: `localhost:4200` or the host of the site. */
export const targetLabel = (target: TSlotTarget): string =>
  target.kind === 'port' ? `localhost:${target.port}` : new URL(target.origin).host;

/** Where a typed or remembered address leads, for the history list. */
export const targetAddress = (target: IUrlTarget): string =>
  `${target.origin}${target.path === '/' ? '' : target.path}`;

const GATEWAY_ERRORS: Record<string, string> = {
  'invalid neighbours': t('lib.hlPreviewClient.invalidNeighbours'),
  'invalid target': t('lib.hlPreviewClient.invalidTarget'),
  'invalid slot': t('lib.hlPreviewClient.invalidSlot'),
  'invalid path': PATH_ERROR,
};

const failureText = async (response: Response): Promise<string> => {
  const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
  const known = typeof body?.error === 'string' ? GATEWAY_ERRORS[body.error] : undefined;
  return known ?? t('lib.hlPreviewClient.openFailed', { status: response.status });
};

export const openRequestBody = (
  target: TPreviewTarget,
  slot: number,
  neighbours: readonly number[] = [],
) => ({
  target:
    target.kind === 'port' ? { kind: 'port', port: target.port } : { kind: 'url', origin: target.origin },
  slot,
  path: target.path,
  ...(target.kind === 'port' && neighbours.length > 0 ? { neighbours: [...neighbours] } : {}),
});

export const openPreview = async (
  target: TPreviewTarget,
  slot = 1,
  neighbours: readonly number[] = [],
  machineId?: string,
): Promise<IOpenedPreview> => {
  let response: Response;
  try {
    response = await appFetch(scopedAppPath('/api/preview/open', machineId), {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(openRequestBody(target, slot, neighbours)),
    });
  } catch (error) {
    // A lapsed Teleport session answers with a cross-origin redirect, which fetch reports as a TypeError.
    if (error instanceof TypeError)
      throw new Error(t('lib.hlPreviewClient.sessionExpired', { host: appHost() }), { cause: error });
    throw error;
  }
  if (response.status === 404) throw new PreviewUnavailableError();
  if (!response.ok) throw new Error(await failureText(response));
  const opened = (await response.json()) as Omit<IOpenedPreview, 'neighbours'>;
  return {
    ...opened,
    neighbours: target.kind === 'port' ? [...neighbours] : [],
    ...(machineId ? { machineId } : {}),
  };
};

/** Listening dev servers of the runtime machine, grouped against the conversation's work paths. */
export const fetchPreviewPorts = async (
  paths: readonly string[],
  signal: AbortSignal,
): Promise<IPreviewPort[]> => {
  const response = await appFetch(
    `/api/platform/preview/ports?paths=${encodeURIComponent(JSON.stringify(paths))}`,
    { signal },
  );
  if (!response.ok) throw new Error(t('lib.hlPreviewClient.portsFailed', { status: response.status }));
  return ((await response.json()) as { ports: IPreviewPort[] }).ports;
};

/** Files of a picked component in the worktree the port is served from. */
export const resolvePickedComponent = async (
  port: number,
  component: IPickedComponent,
): Promise<IResolvedMatch[]> => {
  const response = await appFetch('/api/platform/preview/resolve', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ port, component }),
  });
  if (!response.ok) throw new Error(t('lib.hlPreviewClient.resolveFailed', { status: response.status }));
  return ((await response.json()) as { matches: IResolvedMatch[] }).matches;
};

/**
 * A component of an MFE remote lives in the remote's worktree: the main port is asked first, then its neighbours, one
 * by one. The first non-empty answer wins; a port that fails is skipped, and only all of them failing is a failure.
 */
export const resolveAcrossPorts = async (
  ports: readonly number[],
  component: IPickedComponent,
  resolve: (port: number, component: IPickedComponent) => Promise<IResolvedMatch[]> = resolvePickedComponent,
): Promise<IResolvedMatch[]> => {
  let failure: unknown = null;
  let answered = false;
  for (const port of ports) {
    try {
      const matches = await resolve(port, component);
      if (matches.length > 0) return matches;
      answered = true;
    } catch (error) {
      failure = error;
    }
  }
  if (!answered && failure) throw failure;
  return [];
};

/** Only the newest request may write its result: an older response arriving late is dropped. */
export const createLatestGate = () => {
  let latest = 0;
  return {
    next: (): number => ++latest,
    isLatest: (id: number): boolean => id === latest,
  };
};
