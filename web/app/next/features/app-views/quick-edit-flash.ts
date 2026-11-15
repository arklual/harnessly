// A voice quick edit changed an app's object: the app's open views hear `quick-edit` and light up what moved.
// The page that made the edit tells it here; every frame of that app hears it, with no events: permission (the object
// is the app's own).

export interface IQuickEditFlash {
  object: string;
  ids: string[];
  hint: string;
}

type TListener = (flash: IQuickEditFlash) => void;

const listeners = new Map<string, Set<TListener>>();

/** The edit of the app's object landed: its open views hear it. */
export const flashQuickEdit = (app: string, flash: IQuickEditFlash): void => {
  for (const fn of listeners.get(app) ?? []) fn(flash);
};

/** Hear the quick edits of an app's objects; the function stops it. */
export const watchQuickEdits = (app: string, fn: TListener): (() => void) => {
  const set = listeners.get(app) ?? new Set<TListener>();
  set.add(fn);
  listeners.set(app, set);
  return () => {
    set.delete(fn);
    if (set.size === 0) listeners.delete(app);
  };
};
