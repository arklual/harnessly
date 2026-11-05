// An html file shown as a page has no address of its own: the files it names beside it (styles, scripts, pictures,
// fonts in its css) are read through the chat and put into the page itself, so it looks as it does in a browser.
// Only relative names are read; within a budget, so a page with a huge video does not stall the pane.
// The page's scripts run, so what it may read is fenced: a name never climbs above the chat's folder (a page in it) or
// the page's own folder (a page elsewhere), and a page that got any file of the machine is cut off the network by its
// policy, so what it read cannot be sent anywhere.

/** Reads a file named relative to the page; null when it cannot be read. */
export type TReadAsset = (relative: string) => Promise<Blob | null>;

const MAX_FILES = 60;
const MAX_FILE_BYTES = 8 * 1024 * 1024;
const MAX_TOTAL_BYTES = 24 * 1024 * 1024;

/** A name of a file beside the page: not an address, not a root path, not data. */
export const isRelative = (ref: string | null): ref is string =>
  !!ref && !/^(?:[a-z][a-z0-9+.-]*:|\/|#)/i.test(ref.trim());

/** a/b/../c → a/c; the query and the hash dropped; an absolute base stays absolute. null when the name climbs above
 *  `floor` (a folder the result must stay in, '' for any relative one). */
export const joinRelative = (base: string, ref: string, floor = ''): string | null => {
  const root = base.startsWith('/') ? '/' : '';
  const clean = ref.trim().split(/[?#]/)[0];
  const parts = [...(base ? base.split('/') : []), ...clean.split('/')];
  const out: string[] = [];
  for (const part of parts) {
    if (part === '' || part === '.') continue;
    if (part === '..') {
      if (out.length === 0) return null;
      out.pop();
    } else out.push(part);
  }
  const joined = root + out.join('/');
  const fence = floor.replace(/\/+$/, '');
  if (fence && joined !== fence && !joined.startsWith(`${fence}/`)) return null;
  return joined;
};

/** What a page whose scripts run may reach once it holds files of the machine: nothing outside itself. */
export const SEALED_POLICY =
  "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; media-src data:; " +
  "font-src data:; connect-src 'none'; form-action 'none'; frame-src 'none'; base-uri 'none'";

const dirOf = (path: string) => (path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '');

const asDataUrl = (blob: Blob) =>
  new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error ?? new Error('read'));
    reader.readAsDataURL(blob);
  });

class Budget {
  files = 0;
  bytes = 0;
  taken = 0;
  constructor(
    private readonly read: TReadAsset,
    private readonly floor: string,
  ) {}
  /** The file `ref` names from `base`, or null: outside the fence, unreadable, or over the budget. */
  async take(base: string, ref: string): Promise<Blob | null> {
    const path = joinRelative(base, ref, this.floor);
    if (path === null || this.files >= MAX_FILES || this.bytes >= MAX_TOTAL_BYTES) return null;
    this.files += 1;
    const blob = await this.read(path).catch(() => null);
    if (!blob || blob.size > MAX_FILE_BYTES || this.bytes + blob.size > MAX_TOTAL_BYTES) return null;
    this.bytes += blob.size;
    this.taken += 1;
    return blob;
  }
}

const CSS_URL = /url\(\s*(['"]?)([^'")]+)\1\s*\)/g;

/** A stylesheet with the files it names (fonts, pictures) put into it. `base` is the stylesheet's folder. */
const inlineCss = async (css: string, base: string, budget: Budget): Promise<string> => {
  const refs = [...new Set([...css.matchAll(CSS_URL)].map((m) => m[2]).filter(isRelative))];
  const urls = new Map<string, string>();
  await Promise.all(
    refs.map(async (ref) => {
      const blob = await budget.take(base, ref);
      if (blob) urls.set(ref, await asDataUrl(blob));
    }),
  );
  return css.replace(CSS_URL, (whole, _q: string, ref: string) => {
    const url = urls.get(ref);
    return url ? `url("${url}")` : whole;
  });
};

/** The page with its neighbours put in. `path` is the page's path as the reader names files (its folder is the base). */
export const inlineAssets = async (html: string, path: string, read: TReadAsset): Promise<string> => {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  const base = dirOf(path);
  // A page of the chat's folder reads the folder; a page elsewhere only its own folder.
  const budget = new Budget(read, path.startsWith('/') ? base : '');
  const jobs: Promise<void>[] = [];
  doc.querySelectorAll<HTMLLinkElement>('link[rel~="stylesheet"][href]').forEach((link) => {
    const href = link.getAttribute('href');
    if (!isRelative(href)) return;
    const file = joinRelative(base, href);
    jobs.push(
      budget.take(base, href).then(async (blob) => {
        if (!blob || file === null) return;
        const style = doc.createElement('style');
        style.textContent = await inlineCss(await blob.text(), dirOf(file), budget);
        link.replaceWith(style);
      }),
    );
  });
  doc.querySelectorAll<HTMLScriptElement>('script[src]').forEach((script) => {
    const src = script.getAttribute('src');
    if (!isRelative(src)) return;
    jobs.push(
      budget.take(base, src).then(async (blob) => {
        if (!blob) return;
        script.removeAttribute('src');
        script.textContent = await blob.text();
      }),
    );
  });
  doc
    .querySelectorAll<HTMLElement>('img[src], source[src], video[poster], audio[src], video[src]')
    .forEach((el) => {
      const attr = el.hasAttribute('poster') && !el.hasAttribute('src') ? 'poster' : 'src';
      const src = el.getAttribute(attr);
      if (!isRelative(src)) return;
      jobs.push(
        budget.take(base, src).then(async (blob) => {
          if (blob) el.setAttribute(attr, await asDataUrl(blob));
        }),
      );
    });
  doc.querySelectorAll<HTMLStyleElement>('style').forEach((style) => {
    const css = style.textContent ?? '';
    if (!css.includes('url(')) return;
    jobs.push(
      inlineCss(css, base, budget).then((next) => {
        style.textContent = next;
      }),
    );
  });
  await Promise.all(jobs);
  if (budget.taken > 0) {
    const policy = doc.createElement('meta');
    policy.httpEquiv = 'Content-Security-Policy';
    policy.content = SEALED_POLICY;
    doc.head.prepend(policy);
  }
  return `<!doctype html>\n${doc.documentElement.outerHTML}`;
};
