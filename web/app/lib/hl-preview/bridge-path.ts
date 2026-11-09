/**
 * The page bridge's part for a slot by path (`<appBase>/__pv/<slot>-<key>/` on the platform's own origin): the page runs
 * on an opaque origin in a sandbox, behind a prefix it knows nothing about. Here it gets back what it had on localhost:
 * its own path without the prefix (routers read `location.pathname`), root-absolute addresses that land under the
 * prefix, cookies and a localStorage that work, workers that start. Everything is installed before the page's scripts.
 */
/* eslint-disable @typescript-eslint/no-explicit-any -- it patches the page's window, documents and constructors */

/** A root-absolute address of the page's host gets the prefix; anything else, or one that has it, stays as it is. */
export const withPrefix = (raw: string, prefix: string, base: string, origin: string): string => {
  let url: URL;
  try {
    url = new URL(raw, base);
  } catch {
    return raw;
  }
  const own = url.origin === origin || (url.protocol.startsWith('ws') && url.host === new URL(origin).host);
  if (!own) return raw;
  if (url.pathname === prefix || url.pathname.startsWith(`${prefix}/`)) return url.href;
  url.pathname = prefix + url.pathname;
  return url.href;
};

/** The page's path without the prefix: what it would be on localhost. */
export const withoutPrefix = (path: string, prefix: string): string => {
  if (path === prefix) return '/';
  return path.startsWith(`${prefix}/`) ? path.slice(prefix.length) : path;
};

/** Attributes that hold an address, of any element (data= of an <object> only, srcset as a list of candidates). */
const MARKUP_URL_ATTRS = new Set(['src', 'href', 'action', 'formaction', 'poster', 'background', 'manifest']);
const MARKUP_SRCSET = new Set(['srcset', 'imagesrcset']);
/** Elements whose text the parser does not read as markup: what they hold is a script's or a style's, not addresses. */
const RAW_TEXT = new Set([
  'script',
  'style',
  'textarea',
  'title',
  'xmp',
  'iframe',
  'noembed',
  'noframes',
  'noscript',
]);
const SPACE = /[\t\n\f\r ]/;

/** `fix` on each candidate's address of a srcset, read as the browser reads one (a data: address keeps its commas). */
export const rewriteSrcset = (value: string, fix: (raw: string) => string): string => {
  let out = '';
  let i = 0;
  while (i < value.length) {
    let j = i;
    while (j < value.length && (SPACE.test(value[j]) || value[j] === ',')) j++;
    out += value.slice(i, j);
    if ((i = j) >= value.length) break;
    while (j < value.length && !SPACE.test(value[j])) j++;
    const addr = value.slice(i, j).replace(/,+$/, '');
    out += fix(addr) + value.slice(i + addr.length, j);
    i = j;
    if (value[j - 1] === ',') continue;
    for (let depth = 0; j < value.length && (value[j] !== ',' || depth > 0); j++) {
      if (value[j] === '(') depth++;
      else if (value[j] === ')') depth--;
    }
    out += value.slice(i, j);
    i = j;
  }
  return out;
};

/**
 * The addresses of markup the parser inserts (`document.write`): each start tag's address attributes go through `fix`,
 * quoted or not; comments, the text of scripts and styles, and anything that is not a tag stay as written. A tag cut
 * off at the end of the markup stays as it is.
 */
export const rewriteMarkup = (html: string, fix: (raw: string) => string): string => {
  const tagName = /<([a-zA-Z][^\t\n\f\r />]*)/y;
  const spaces = /[\t\n\f\r /]*/y;
  const attrName = /[^\t\n\f\r />][^\t\n\f\r />=]*/y;
  const equals = /[\t\n\f\r ]*=[\t\n\f\r ]*/y;
  const unquoted = /[^\t\n\f\r >]+/y;
  const at = (re: RegExp, from: number) => {
    re.lastIndex = from;
    return re.exec(html) ? re.lastIndex : from;
  };
  let out = '';
  let i = 0;
  while (i < html.length) {
    const lt = html.indexOf('<', i);
    if (lt < 0) return out + html.slice(i);
    out += html.slice(i, lt);
    i = lt;
    if (html.startsWith('<!--', i)) {
      const end = html.indexOf('-->', i + 4);
      const stop = end < 0 ? html.length : end + 3;
      out += html.slice(i, stop);
      i = stop;
      continue;
    }
    tagName.lastIndex = i;
    const name = tagName.exec(html)?.[1].toLowerCase();
    if (!name) {
      out += '<';
      i += 1;
      continue;
    }
    let j = tagName.lastIndex;
    let tag = html.slice(i, j);
    let closed = false;
    while (j < html.length) {
      const k = at(spaces, j);
      tag += html.slice(j, k);
      j = k;
      if (html[j] === '>') {
        tag += '>';
        j += 1;
        closed = true;
        break;
      }
      const nameEnd = at(attrName, j);
      if (nameEnd === j) break;
      const attr = html.slice(j, nameEnd).toLowerCase();
      tag += html.slice(j, nameEnd);
      j = nameEnd;
      const eq = at(equals, j);
      if (eq === j) continue;
      tag += html.slice(j, eq);
      j = eq;
      const quote = html[j] === '"' || html[j] === "'" ? html[j] : '';
      const start = quote ? j + 1 : j;
      const end = quote ? html.indexOf(quote, start) : at(unquoted, j);
      if (end < 0) {
        tag += html.slice(j);
        j = html.length;
        break;
      }
      const raw = html.slice(start, end);
      let value = raw;
      if (MARKUP_URL_ATTRS.has(attr) || (attr === 'data' && name === 'object')) value = fix(raw);
      else if (MARKUP_SRCSET.has(attr)) value = rewriteSrcset(raw, fix);
      tag += quote + value + quote;
      j = quote ? end + 1 : end;
    }
    out += tag;
    i = j;
    if (closed && RAW_TEXT.has(name)) {
      const close = new RegExp(`</${name}[\\t\\n\\f\\r />]`, 'ig');
      close.lastIndex = i;
      const found = close.exec(html);
      const stop = found ? found.index : html.length;
      out += html.slice(i, stop);
      i = stop;
    }
  }
  return out;
};

interface IStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
  clear(): void;
  key(index: number): string | null;
  readonly length: number;
}

/** A Storage over a map: `changed` hears every write (localStorage writes back to the runtime). */
export const makeStorage = (items: Record<string, string>, changed: () => void): IStore => {
  const map = new Map(Object.entries(items));
  return {
    getItem: (key) => (map.has(String(key)) ? (map.get(String(key)) as string) : null),
    setItem: (key, value) => {
      map.set(String(key), String(value));
      changed();
    },
    removeItem: (key) => {
      if (map.delete(String(key))) changed();
    },
    clear: () => {
      map.clear();
      changed();
    },
    key: (index) => Array.from(map.keys())[index] ?? null,
    get length() {
      return map.size;
    },
    toJSON: () => Object.fromEntries(map),
  } as IStore;
};

/** `document.cookie` over a jar of name → value: attributes are read for expiry only, the page sees `a=1; b=2`. */
export const makeCookieJar = (
  seed: Record<string, string>,
  changed: (jar: Record<string, string>) => void,
) => {
  const jar: Record<string, string> = { ...seed };
  return {
    get: () =>
      Object.entries(jar)
        .map(([k, v]) => `${k}=${v}`)
        .join('; '),
    set: (raw: string) => {
      const [pair, ...attrs] = String(raw).split(';');
      const eq = pair.indexOf('=');
      const name = (eq < 0 ? '' : pair.slice(0, eq)).trim();
      const value = (eq < 0 ? pair : pair.slice(eq + 1)).trim();
      if (!name && !value) return;
      const gone = attrs.some((a) => {
        const [k, v = ''] = a.split('=');
        const key = k.trim().toLowerCase();
        if (key === 'max-age') return Number(v.trim()) <= 0;
        if (key === 'expires') return Date.parse(v.trim()) <= Date.now();
        return false;
      });
      if (gone) delete jar[name];
      else jar[name] = value;
      changed(jar);
    },
  };
};

const COOKIE_KEY = '\u0000hl-cookies';
const FLUSH_MS = 300;

/** Installs everything the page by path needs. `win` is the page's window, before its scripts run. */
export const installPathMode = (win: any, doc: any, prefix: string): void => {
  const origin: string = win.location.origin;
  const loc = win.location;
  const NativeXHR = win.XMLHttpRequest;
  const xhrOpen = NativeXHR?.prototype?.open;
  const nativeFetch = typeof win.fetch === 'function' ? win.fetch.bind(win) : null;
  const beacon = win.navigator?.sendBeacon?.bind(win.navigator);
  const History = win.History?.prototype;
  const nativePush = History?.pushState;
  const nativeReplace = History?.replaceState;
  const base = () => doc.baseURI || loc.href;
  const fix = (raw: unknown) => (typeof raw === 'string' ? withPrefix(raw, prefix, base(), origin) : raw);
  const isOwn = (raw: string) => {
    try {
      return new URL(raw, base()).origin === origin;
    } catch {
      return false;
    }
  };
  const storageURL = `${origin}${prefix}/__tk/storage`;

  // Storage: read once and synchronously, so it is there before the page's first script.
  let items: Record<string, string> = {};
  try {
    const xhr = new NativeXHR();
    xhrOpen.call(xhr, 'GET', storageURL, false);
    xhr.send();
    if (xhr.status === 200) items = JSON.parse(xhr.responseText) || {};
  } catch {
    items = {};
  }
  let timer: any = null;
  const store: { local: IStore | null } = { local: null };
  const flush = (onExit = false) => {
    timer = null;
    const body = JSON.stringify(store.local);
    if (onExit && beacon) beacon(storageURL, new win.Blob([body], { type: 'text/plain' }));
    else if (nativeFetch)
      nativeFetch(storageURL, { method: 'POST', body, keepalive: true }).catch(() => undefined);
  };
  const local = makeStorage(items, () => {
    if (timer === null) timer = win.setTimeout(() => flush(), FLUSH_MS);
  });
  store.local = local;
  const session = makeStorage({}, () => undefined);
  for (const [name, store] of [
    ['localStorage', local],
    ['sessionStorage', session],
  ] as const) {
    try {
      Object.defineProperty(win, name, { configurable: true, get: () => store });
    } catch {
      // A browser that will not let it be redefined keeps its own (and the page its error).
    }
  }
  win.addEventListener?.('pagehide', () => {
    if (timer !== null) {
      win.clearTimeout(timer);
      flush(true);
    }
  });

  // document.cookie: an opaque origin has none; the page's own cookies live in a jar kept with its storage. The
  // server's cookies (HttpOnly ones above all) still travel with the requests below.
  let seed: Record<string, string> = {};
  try {
    seed = JSON.parse(local.getItem(COOKIE_KEY) || '{}') || {};
  } catch {
    seed = {};
  }
  const jar = makeCookieJar(seed, (all) => local.setItem(COOKIE_KEY, JSON.stringify(all)));
  try {
    Object.defineProperty(doc, 'cookie', { configurable: true, get: jar.get, set: jar.set });
  } catch {
    // Left as the browser has it.
  }

  // The address the page sees: its path on localhost.
  const visible = withoutPrefix(loc.pathname, prefix);
  if (nativeReplace && visible !== loc.pathname) {
    nativeReplace.call(win.history, win.history.state, '', visible + loc.search + loc.hash);
  }
  if (History) {
    // A router pushes the paths it knows, without the prefix; one given with the prefix (Angular's base href) loses it.
    const plain = (url: unknown) => {
      if (typeof url !== 'string' && !(url instanceof win.URL)) return url;
      try {
        const u = new URL(String(url), loc.href);
        return u.origin === origin ? withoutPrefix(u.pathname, prefix) + u.search + u.hash : url;
      } catch {
        return url;
      }
    };
    History.pushState = function (state: unknown, title: string, url?: unknown) {
      return nativePush.call(this, state, title, url === undefined ? url : plain(url));
    };
    History.replaceState = function (state: unknown, title: string, url?: unknown) {
      return nativeReplace.call(this, state, title, url === undefined ? url : plain(url));
    };
  }

  // Requests: root-absolute addresses of the host get the prefix, and the page's own requests carry its cookies.
  if (nativeFetch) {
    win.fetch = (input: any, init?: any) => {
      if (typeof input === 'string' || input instanceof win.URL) {
        const url = fix(String(input)) as string;
        const options = isOwn(url) && !init?.credentials ? { ...init, credentials: 'include' } : init;
        return nativeFetch(url, options);
      }
      if (input instanceof win.Request) {
        const url = fix(input.url) as string;
        if (url !== input.url || isOwn(url)) {
          return nativeFetch(
            new win.Request(url, input),
            init ?? (isOwn(url) ? { credentials: 'include' } : undefined),
          );
        }
      }
      return nativeFetch(input, init);
    };
  }
  if (xhrOpen) {
    NativeXHR.prototype.open = function (method: string, url: unknown, ...rest: unknown[]) {
      const fixed = fix(url instanceof win.URL ? String(url) : url);
      const out = xhrOpen.call(this, method, fixed, ...rest);
      try {
        if (typeof fixed === 'string' && isOwn(fixed)) this.withCredentials = true;
      } catch {
        // A synchronous request may not change it.
      }
      return out;
    };
  }
  const wrapCtor = (name: string, adjust: (url: string, rest: unknown[]) => unknown[]) => {
    const Native = win[name];
    if (typeof Native !== 'function') return;
    const Wrapped = function (this: unknown, url: unknown, ...rest: unknown[]) {
      const fixed = fix(url instanceof win.URL ? String(url) : url) as string;
      return new Native(fixed, ...adjust(fixed, rest));
    } as any;
    Wrapped.prototype = Native.prototype;
    Object.setPrototypeOf(Wrapped, Native);
    win[name] = Wrapped;
  };
  wrapCtor('WebSocket', (_url, rest) => rest);
  wrapCtor('EventSource', (url, rest) =>
    isOwn(url) ? [{ ...(rest[0] as object), withCredentials: true }] : rest,
  );
  if (beacon) win.navigator.sendBeacon = (url: unknown, data?: unknown) => beacon(fix(url), data);

  // Workers: an opaque origin may only start one from a blob of its own, which loads the real script.
  for (const name of ['Worker', 'SharedWorker']) {
    const Native = win[name];
    if (typeof Native !== 'function') continue;
    const Wrapped = function (this: unknown, url: unknown, options?: any) {
      const real = fix(url instanceof win.URL ? String(url) : url) as string;
      if (typeof real !== 'string' || real.startsWith('blob:') || real.startsWith('data:'))
        return new Native(real, options);
      const module = options?.type === 'module';
      const code = module ? `import ${JSON.stringify(real)};` : `importScripts(${JSON.stringify(real)});`;
      const blob = win.URL.createObjectURL(new win.Blob([code], { type: 'text/javascript' }));
      return new Native(blob, options);
    } as any;
    Wrapped.prototype = Native.prototype;
    Object.setPrototypeOf(Wrapped, Native);
    win[name] = Wrapped;
  }

  // Markup the parser inserts (document.write, writeln) loads past the element hooks below: its addresses get the
  // prefix before it is written, relative ones resolved against the document's base (the path without the prefix).
  // A fragment, an address of another host and one that has the prefix are left as written.
  const Doc = win.Document?.prototype;
  for (const name of ['write', 'writeln'] as const) {
    const native = Doc?.[name];
    if (typeof native !== 'function') continue;
    Doc[name] = function (this: any, ...parts: unknown[]) {
      // TrustedHTML and anything else that is not a string goes as it came.
      if (!parts.length || parts.some((part) => typeof part !== 'string')) return native.apply(this, parts);
      const at = (this?.baseURI as string) || base();
      const markupFix = (raw: string) => {
        const value = raw.trim();
        if (!value || value.startsWith('#')) return raw;
        let url: URL;
        try {
          url = new URL(value, at);
        } catch {
          return raw;
        }
        if (url.origin !== origin || url.pathname === prefix || url.pathname.startsWith(`${prefix}/`))
          return raw;
        const lead = raw.slice(0, raw.indexOf(value));
        return lead + withPrefix(value, prefix, at, origin) + raw.slice(lead.length + value.length);
      };
      return native.call(this, rewriteMarkup((parts as string[]).join(''), markupFix));
    };
  }

  // Addresses a script gives to elements: src, href and action of the host get the prefix.
  const Element = win.Element?.prototype;
  const setAttribute = Element?.setAttribute;
  const urlAttrs = new Set(['src', 'href', 'action', 'formaction', 'poster']);
  if (setAttribute) {
    Element.setAttribute = function (name: string, value: unknown) {
      return setAttribute.call(
        this,
        name,
        urlAttrs.has(String(name).toLowerCase()) ? fix(String(value)) : value,
      );
    };
  }
  for (const [ctor, prop] of [
    ['HTMLScriptElement', 'src'],
    ['HTMLImageElement', 'src'],
    ['HTMLIFrameElement', 'src'],
    ['HTMLSourceElement', 'src'],
    ['HTMLMediaElement', 'src'],
    ['HTMLLinkElement', 'href'],
    ['HTMLAnchorElement', 'href'],
    ['HTMLFormElement', 'action'],
  ] as const) {
    const proto = win[ctor]?.prototype;
    const desc = proto && Object.getOwnPropertyDescriptor(proto, prop);
    if (!desc?.set) continue;
    Object.defineProperty(proto, prop, {
      configurable: true,
      enumerable: desc.enumerable,
      get: desc.get,
      set(value: unknown) {
        desc.set!.call(this, fix(String(value)));
      },
    });
  }
};
