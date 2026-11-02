// An app tab's target: the app's id and, after a slash, one of its views (internal/panel and the runtime hold the same).

export const APP_TARGET_RE =
  /^([a-z0-9][a-z0-9-]{0,39}\.[a-z0-9][a-z0-9-]{0,39})(?:\/([a-z0-9][a-z0-9_-]{0,39}))?$/;

/** The app and view an `app` tab's target names; null for a target that is not one. */
export const parseAppTarget = (target: string | null): { app: string; view: string | null } | null => {
  const m = target === null ? null : APP_TARGET_RE.exec(target);
  return m ? { app: m[1], view: m[2] ?? null } : null;
};
