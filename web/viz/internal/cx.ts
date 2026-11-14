// SPDX-License-Identifier: AGPL-3.0-only

/** Joins class names, skipping empty values. */
export function classNames(...parts: readonly (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ');
}
