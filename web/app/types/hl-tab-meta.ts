import type { ILaunchChoice } from '@/types/hl-launch';

/**
 * Hl metadata of a layout tab, stored under the runtime's tab metadata key (RUNTIME_TAB_META_KEY) in the layout file on disk.
 * Written only on the server through `updateTabHl`; the client reads it from the layout and GET conversations.
 */

export const TAB_MODES = ['automation'] as const;
export type TTabMode = (typeof TAB_MODES)[number];

export interface ITabHl {
  launch?: ILaunchChoice;
  mode?: TTabMode;
}

export const TAB_HL_KEYS = ['launch', 'mode'] as const satisfies readonly (keyof ITabHl)[];

/** Fields GET /api/platform/conversations returns for every agent conversation. */
export type IConversationMeta = Pick<ITabHl, (typeof TAB_HL_KEYS)[number]>;

const isPlainObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const sameValue = (a: unknown, b: unknown): boolean => {
  if (a === b) return true;
  if (!isPlainObject(a) || !isPlainObject(b)) return false;
  const keys = Object.keys(a);
  if (keys.length !== Object.keys(b).length) return false;
  return keys.every((key) => Object.prototype.hasOwnProperty.call(b, key) && sameValue(a[key], b[key]));
};

/**
 * Shallow merge: every key present in the patch with a defined value replaces the stored one,
 * `undefined` leaves it alone. `changed` is false when nothing differs,
 * so the caller can skip the write.
 */
export const mergeTabHl = (
  current: ITabHl | undefined,
  patch: Partial<ITabHl>,
): { next: ITabHl; changed: boolean } => {
  const next: ITabHl = { ...current };
  let changed = false;
  for (const key of TAB_HL_KEYS) {
    const value = patch[key];
    if (value === undefined || sameValue(next[key], value)) continue;
    (next as Record<string, unknown>)[key] = value;
    changed = true;
  }
  return { next, changed };
};

const isNullableString = (value: unknown): value is string | null =>
  value === null || typeof value === 'string';

const readLaunch = (value: unknown): ILaunchChoice | undefined => {
  if (!isPlainObject(value) || !isNullableString(value.model) || !isNullableString(value.effort))
    return undefined;
  return { model: value.model, effort: value.effort };
};

/**
 * The layout file is plain JSON on disk: only well-formed fields reach the API answer, each one on its own,
 * and absent fields stay absent (no `undefined` keys in the JSON).
 */
export const conversationMeta = (raw: unknown): IConversationMeta => {
  if (!isPlainObject(raw)) return {};
  const meta: IConversationMeta = {};
  const launch = readLaunch(raw.launch);
  if (launch) meta.launch = launch;
  if ((TAB_MODES as readonly unknown[]).includes(raw.mode)) meta.mode = raw.mode as TTabMode;
  return meta;
};
