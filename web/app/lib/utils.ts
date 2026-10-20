/** Joins class names, skipping empty ones. */
export const cn = (...parts: Array<string | false | null | undefined | 0>): string =>
  parts.filter(Boolean).join(' ');
