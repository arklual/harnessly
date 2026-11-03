// SPDX-License-Identifier: Apache-2.0

/** Joins class names, skipping empty values. */
export function classNames(...parts: readonly (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(' ');
}
