import { useSyncExternalStore } from 'react';

/** A phone or tablet (touch first), whatever the window width. */
const isTouchDevice = (): boolean =>
  typeof navigator !== 'undefined' &&
  (/Android|iPhone|iPad|iPod|Mobile/i.test(navigator.userAgent) ||
    (navigator.maxTouchPoints > 1 && /Macintosh/.test(navigator.userAgent)));

const never = () => () => {};

export default function useIsMobileDevice(): boolean {
  return useSyncExternalStore(never, isTouchDevice, () => false);
}
