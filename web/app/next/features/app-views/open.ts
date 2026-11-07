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

type TAddressListener = (address: string) => void;

const addressListeners = new Set<TAddressListener>();

/** Opens a local address (`localhost:<port><path>`) in the pane's browser: a view's hl.preview.open. */
export const openAddressTab = (address: string): boolean => {
  if (addressListeners.size === 0) return false;
  addressListeners.forEach((listen) => listen(address));
  return true;
};

export const onOpenAddressTab = (listen: TAddressListener): (() => void) => {
  addressListeners.add(listen);
  return () => addressListeners.delete(listen);
};
