// The build a page runs and the one the server serves now, compared by the entry script the build names by its content.
const SCRIPT_TAG_RE = /<script\b[^>]*>/gi;
const TYPE_MODULE_RE = /\btype\s*=\s*["']?module\b/i;
const SRC_RE = /\bsrc\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/i;

/** The `src` of every module script in a served page, in order. */
export const moduleScriptSources = (html: string): string[] => {
  const found: string[] = [];
  for (const tag of html.match(SCRIPT_TAG_RE) ?? []) {
    if (!TYPE_MODULE_RE.test(tag)) continue;
    const match = SRC_RE.exec(tag);
    const src = match?.[1] ?? match?.[2] ?? match?.[3];
    if (src) found.push(src);
  }
  return found;
};

/**
 * The page's entry script: the build names it by its content (`assets/index-<hash>.js`), so a new release
 * is a new address. It is the first module script, resolved against the page address so a relative and an
 * absolute spelling compare equal.
 */
export const entryScriptOf = (sources: readonly string[], pageUrl: string): string | null => {
  const first = sources[0];
  if (!first) return null;
  try {
    return new URL(first, pageUrl).pathname;
  } catch {
    return null;
  }
};

/** The entry script of a served page (its HTML text). */
export const entryScriptOfPage = (html: string, pageUrl: string): string | null =>
  entryScriptOf(moduleScriptSources(html), pageUrl);

/** A newer build is out when the server's page names an entry script and it is not the one running here. */
export const isNewerBuild = (running: string | null | undefined, served: string | null): boolean =>
  Boolean(running && served && running !== served);
