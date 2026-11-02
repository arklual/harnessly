// An app's tab asked for from anywhere (a view's hl.pane.open, a card's «Открыть в панели»): the pane listens while it is
// mounted and places the tab. Same shape as the subagents' open (features/subagents/open.ts).
import { parseAppTarget } from '@/next/features/app-views/target';

type TListener = (target: string) => void;

const listeners = new Set<TListener>();

/** Opens `<app id>[/<view>]` in the pane; false for a target that is not one. */
export const openAppTab = (target: string): boolean => {
  const named = target.startsWith('app:') ? target.slice(4) : target;
  if (parseAppTarget(named) === null) return false;
  listeners.forEach((listen) => listen(named));
  return true;
};

export const onOpenAppTab = (listen: TListener): (() => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};
