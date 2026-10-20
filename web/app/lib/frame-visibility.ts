/**
 * A machine frame hidden by the shell around it still reports `document.visibilityState === 'visible'`:
 * the shell tells it instead, and background work here treats that like a hidden browser tab.
 */
let frameHidden = false;
const listeners = new Set<() => void>();

export const isFrameHidden = () => frameHidden;

export const setFrameHidden = (hidden: boolean) => {
  if (frameHidden === hidden) return;
  frameHidden = hidden;
  listeners.forEach((listener) => listener());
};

export const subscribeFrameVisibility = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const isPageHidden = () =>
  frameHidden || (typeof document !== 'undefined' && document.visibilityState === 'hidden');
