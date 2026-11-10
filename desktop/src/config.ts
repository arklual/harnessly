export type ServerMode = 'local' | 'remote';

export interface ServerConfig {
  mode: ServerMode;
  remoteUrl?: string;
}

export interface WindowState {
  x?: number;
  y?: number;
  width: number;
  height: number;
  isMaximized?: boolean;
  isFullScreen?: boolean;
  displayId?: number;
}

export interface AppConfig {
  server?: ServerConfig;
  recentRemoteUrls?: string[];
  windowState?: WindowState;
  appTheme?: string;
  locale?: string;
  deepLinks?: boolean;
}

export const DEFAULT_LOCAL_PORT = 8022;
export const MAX_RECENT_REMOTE_URLS = 10;
export const DEFAULT_WINDOW_STATE: WindowState = { width: 1280, height: 800 };

export const parseAppConfig = (raw: string): AppConfig => {
  try {
    const value = JSON.parse(raw);
    return value && typeof value === 'object' && !Array.isArray(value) ? (value as AppConfig) : {};
  } catch {
    return {};
  }
};

export const serverConfigOf = (cfg: AppConfig): ServerConfig =>
  cfg.server?.mode === 'remote' && cfg.server.remoteUrl
    ? { mode: 'remote', remoteUrl: cfg.server.remoteUrl }
    : { mode: 'local' };

/** Accepts "host", "host:port" and full http(s) URLs; returns the origin or null. */
export const normalizeRemoteUrl = (input: string): string | null => {
  const text = input.trim();
  if (!text) return null;
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(text) ? text : `https://${text}`;
  try {
    const url = new URL(withScheme);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
    return url.origin + (url.pathname === '/' ? '' : url.pathname.replace(/\/$/, ''));
  } catch {
    return null;
  }
};

export const addRecentRemoteUrl = (list: string[] | undefined, url: string): string[] =>
  [url, ...(list ?? []).filter((item) => item !== url)].slice(0, MAX_RECENT_REMOTE_URLS);

export const removeRecentRemoteUrl = (list: string[] | undefined, url: string): string[] =>
  (list ?? []).filter((item) => item !== url);

export const localUrl = (port: number): string => `http://127.0.0.1:${port}`;

/** HARNESSLY_DESKTOP_URL (e.g. a staging hub) wins over the saved choice; empty means "ask". */
export const resolveStartUrl = (
  cfg: AppConfig,
  env: Record<string, string | undefined>,
  localPort: number | null,
): string | null => {
  const forced = env.HARNESSLY_DESKTOP_URL ? normalizeRemoteUrl(env.HARNESSLY_DESKTOP_URL) : null;
  if (forced) return forced;
  const server = serverConfigOf(cfg);
  if (server.mode === 'remote') return normalizeRemoteUrl(server.remoteUrl ?? '');
  return localPort ? localUrl(localPort) : null;
};
