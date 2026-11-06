// The hub may be served under a path (https://example.com/harnessly/): the server writes it into the page as
// <meta name="hub-base" content="/harnessly">, and it is "" at the root of a host. The screens know only paths without it
// (/s/team/machines); this is the one place that adds it on the way out (requests, links, navigation) and takes it off
// on the way in (location).

const read = (): string => {
  if (typeof document === 'undefined') return '';
  const v = document.querySelector('meta[name="hub-base"]')?.getAttribute('content') ?? '';
  return /^\/[^?#\s]*[^/?#\s]$/.test(v) ? v : '';
};

export const BASE = read();

/** A path of the hub with the base put on it; anything else (another origin, a relative path, a data URL) as it was. */
export const withBase = (url: string, base: string = BASE): string =>
  base &&
  url.startsWith('/') &&
  !url.startsWith('//') &&
  url !== base &&
  !url.startsWith(`${base}/`) &&
  !url.startsWith(`${base}?`)
    ? base + url
    : url;

/** A location's path without the base: the path the screens route on. */
export const withoutBase = (path: string, base: string = BASE): string => {
  if (!base) return path;
  if (path === base) return '/';
  return path.startsWith(`${base}/`) ? path.slice(base.length) : path;
};

/** Makes the page's own requests go under the base: fetch and EventSource take a path of the hub as they always did. */
export const installBase = (base: string = BASE): void => {
  if (!base) return;
  const f = window.fetch.bind(window);
  window.fetch = (input, init) => {
    if (typeof input === 'string') return f(withBase(input, base), init);
    if (input instanceof URL) return f(input, init);
    const url = new URL(input.url);
    if (url.origin === location.origin && !(url.pathname === base || url.pathname.startsWith(`${base}/`))) {
      url.pathname = base + url.pathname;
      return f(new Request(url, input), init);
    }
    return f(input, init);
  };
  const ES = window.EventSource;
  window.EventSource = class extends ES {
    constructor(url: string | URL, init?: EventSourceInit) {
      super(typeof url === 'string' ? withBase(url, base) : url, init);
    }
  };
};
