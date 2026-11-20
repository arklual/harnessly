// An app tab's target: the app's id, an optional view and, after it, an optional object of that view.
import { APP_OBJECT_RE } from '@/next/routes/app-object';

export const APP_TARGET_RE =
  /^([a-z0-9][a-z0-9-]{0,39}\.[a-z0-9][a-z0-9-]{0,39})(?:\/([a-z0-9][a-z0-9_-]{0,39}))?(?:\/([A-Za-z0-9][A-Za-z0-9._-]{0,127}))?$/;

/** The app, view and optional object an `app` tab's target names; null for a target that is not one. */
export const parseAppTarget = (
  target: string | null,
): { app: string; view: string | null; object?: string } | null => {
  const m = target === null ? null : APP_TARGET_RE.exec(target);
  return m ? { app: m[1], view: m[2] ?? null, ...(m[3] ? { object: m[3] } : {}) } : null;
};

/** One of the app's own objects to open as its main thing (`object:<slug>`), or the app's home (`object:`); undefined
 *  for a target that is not one. */
export const objectTarget = (target: string): string | null | undefined => {
  if (!target.startsWith('object:')) return undefined;
  const object = target.slice('object:'.length);
  if (object === '') return null;
  return APP_OBJECT_RE.test(object) ? object : undefined;
};

/** «Поделиться» of one of the app's objects (`share:<slug>`); null for a target that is not one. */
export const shareTarget = (target: string): string | null => {
  if (!target.startsWith('share:')) return null;
  const object = target.slice('share:'.length);
  return APP_OBJECT_RE.test(object) ? object : null;
};
