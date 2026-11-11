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

/** A build this browser ran or was offered, and when it last did. */
export interface ISeenBuild {
  build: string;
  at: number;
}

/** A rollout flips between two or three builds, never more. */
export const SEEN_BUILDS_MAX = 8;
/** How long a build this browser already had is not offered again. */
export const SEEN_BUILD_MS = 6 * 60 * 60_000;

/** The list with `build` seen at `now`: newest last, one entry per build, capped. */
export const rememberBuild = (seen: readonly ISeenBuild[], build: string, now: number): ISeenBuild[] =>
  [...seen.filter((s) => s.build !== build), { build, at: now }].slice(-SEEN_BUILDS_MAX);

/**
 * A build worth offering: the server names one, it is not the one running here, and this browser has not run it or been
 * offered it lately. While a rollout is half done two servers answer for the same address in turn, each with its own
 * page; without the memory the banner comes back after every reload, offering the page the person has just left.
 */
export const isOfferableBuild = (
  running: string | null | undefined,
  served: string | null,
  seen: readonly ISeenBuild[],
  now: number,
): boolean =>
  isNewerBuild(running, served) && !seen.some((s) => s.build === served && now - s.at < SEEN_BUILD_MS);

const isSeenBuild = (s: unknown): s is ISeenBuild =>
  typeof s === 'object' &&
  s !== null &&
  'build' in s &&
  'at' in s &&
  typeof s.build === 'string' &&
  typeof s.at === 'number';

/** The stored list as it was written, or empty: anything else in the key is not ours. */
export const parseSeenBuilds = (raw: string | null): ISeenBuild[] => {
  if (!raw) return [];
  try {
    const value: unknown = JSON.parse(raw);
    return Array.isArray(value) ? value.filter(isSeenBuild) : [];
  } catch {
    return [];
  }
};
