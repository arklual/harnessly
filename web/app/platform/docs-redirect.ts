// The reader's address of a document, `/docs/<slug>`, opens the document in the documents app's own view
// (`/apps/<docs app>?o=<slug>`), in Sinopia's stage with its chat. The reader stays for what only it does yet: a
// version (`?v=`) or a thread (`?thread=`, `?focus=`, a push's click), and for `?legacy=1` — the app page sends there
// when the documents app is not installed.

/** The documents app's id, as its package names it. */
export const DOCS_APP = 'harnessly.docs';

const DOC_RE = /^\/docs\/([^/]+)\/?$/;
const SLUG_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const READER_ONLY = ['v', 'thread', 'focus', 'legacy'] as const;

/** The app's address for an inner path of the reader (no base), or null where the reader stays. */
export const docsAppPath = (inner: string, search: string): string | null => {
  const match = DOC_RE.exec(inner);
  if (!match) return null;
  let slug: string;
  try {
    slug = decodeURIComponent(match[1] ?? '');
  } catch {
    return null;
  }
  if (!SLUG_RE.test(slug)) return null;
  const query = new URLSearchParams(search);
  if (READER_ONLY.some((key) => query.has(key))) return null;
  return `/apps/${DOCS_APP}?${new URLSearchParams({ o: slug }).toString()}`;
};

/** The reader for a document whose app is not here: the same address, kept by the reader. */
export const docsReaderPath = (slug: string): string =>
  `/docs/${encodeURIComponent(slug)}?${new URLSearchParams({ legacy: '1' }).toString()}`;
