import { useEffect, useState } from 'react';

/**
 * Keeps something mounted while it plays its closing animation: `closing` is true from the close request until `ms`
 * later. Every popover, sheet and drawer of the hub closes through this (ease-out in, shorter ease-in out).
 */
export const useClosing = (open: boolean, ms = 160): { mounted: boolean; closing: boolean } => {
  const [done, setDone] = useState(!open);
  const [was, setWas] = useState(open);
  if (open !== was) {
    setWas(open);
    if (open) setDone(false);
  }
  useEffect(() => {
    if (open) return;
    const id = window.setTimeout(() => setDone(true), ms);
    return () => window.clearTimeout(id);
  }, [open, ms]);
  return { mounted: open || !done, closing: !open && !done };
};
