// Which groups of the right column the person has open, kept per browser (like the sidebar's fold in bindings/rail.ts).
// The subagents and the workflows start open, the commands in the background folded: they are mostly a dev server nobody needs to read.
import { useSyncExternalStore } from 'react';

export const FOLD_GROUP = { subagent: 'subagent', workflow: 'workflow', command: 'command' } as const;
export type TFoldGroup = (typeof FOLD_GROUP)[keyof typeof FOLD_GROUP];

const KEY = 'hx-rail-groups';
const OPEN_AT_FIRST: Record<TFoldGroup, boolean> = { subagent: true, workflow: true, command: false };

type TChoices = Partial<Record<TFoldGroup, boolean>>;

const listeners = new Set<() => void>();
let choices: TChoices | null = null;

const read = (): TChoices => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(KEY) ?? '{}');
    if (typeof raw !== 'object' || raw === null) return {};
    const out: TChoices = {};
    for (const group of Object.values(FOLD_GROUP)) {
      const value = (raw as Record<string, unknown>)[group];
      if (typeof value === 'boolean') out[group] = value;
    }
    return out;
  } catch {
    return {};
  }
};

const write = (value: TChoices) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(value));
  } catch {
    // Private mode or full storage: the group still folds for this page.
  }
};

const isOpen = (group: TFoldGroup): boolean => {
  choices ??= read();
  return choices[group] ?? OPEN_AT_FIRST[group];
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** Whether a group is open: the person's last choice in this browser, else how the group starts. */
export const useGroupOpen = (group: TFoldGroup): boolean =>
  useSyncExternalStore(
    subscribe,
    () => isOpen(group),
    () => OPEN_AT_FIRST[group],
  );

export const toggleGroup = (group: TFoldGroup) => {
  choices = { ...(choices ?? read()), [group]: !isOpen(group) };
  write(choices);
  for (const listener of [...listeners]) listener();
};

/** For tests: forget what this page remembers, the storage stays as it is. */
export const forgetFolds = () => {
  choices = null;
  for (const listener of [...listeners]) listener();
};
