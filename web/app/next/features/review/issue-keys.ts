// Tracker tasks a change names: keys in its title and its commits' titles, each once, in the order they first appear,
// by the tracker's key pattern (appConfig().taskKeyPattern), not inside words or paths.
export const issueKeysOf = (texts: readonly (string | null | undefined)[], pattern: string): string[] => {
  const re = new RegExp(String.raw`(?<![\w/.-])(?:${pattern})(?![\w-])`, 'g');
  const seen = new Set<string>();
  for (const text of texts) {
    if (!text) continue;
    for (const match of text.matchAll(re)) seen.add(match[0]);
  }
  return [...seen];
};

/** The tracker's page of a task, or null when the deployment names no tracker. */
export const issueLink = (key: string, template: string | null): string | null =>
  template ? template.replace('{key}', encodeURIComponent(key)) : null;
