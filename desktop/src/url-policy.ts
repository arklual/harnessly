const VALID_URI_SCHEME = /^[a-z][a-z0-9+.-]*:/i;
const BLOCKED_SCHEME = /^(javascript|data|vbscript|blob|file|about|view-source):/i;

export const isSafeExternalUrl = (url: unknown): url is string =>
  typeof url === 'string' && VALID_URI_SCHEME.test(url) && !BLOCKED_SCHEME.test(url);

export const isLoopbackUrl = (raw: string): boolean => {
  try {
    const url = new URL(raw);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return false;
    const host = url.hostname.toLowerCase();
    return host === 'localhost' || host === '127.0.0.1' || host === '::1' || host === '[::1]';
  } catch {
    return false;
  }
};

/** Navigation stays in the window only inside the origin the window currently shows. */
export const isSameOrigin = (current: string, target: string): boolean => {
  try {
    return new URL(current).origin === new URL(target).origin;
  } catch {
    return false;
  }
};

export const isThemeSource = (value: unknown): value is 'dark' | 'light' | 'system' =>
  value === 'dark' || value === 'light' || value === 'system';
