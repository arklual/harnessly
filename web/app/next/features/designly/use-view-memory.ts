import { useCallback, useEffect, useRef, useState } from 'react';
import {
  readView,
  SAVE_MS,
  VIEW_DEFAULT,
  writeView,
  type IViewMemory,
} from '@/next/features/designly/view-memory';

export interface IViewSaver {
  /** What was kept for the design when it opened. */
  initial: IViewMemory;
  /** Joins the change to what is kept and writes it after a quiet; the same view is not written twice. */
  save: (change: Partial<IViewMemory>) => void;
}

/** The kept view of a design (none without a design) and the way to keep it; a pending write is made on leaving. */
export const useViewMemory = (design: string | null): IViewSaver => {
  const [initial] = useState(() => (design === null ? VIEW_DEFAULT : readView(design)));
  const current = useRef<IViewMemory>(initial);
  const written = useRef(JSON.stringify(initial));
  const timer = useRef(0);

  const flush = useCallback(() => {
    window.clearTimeout(timer.current);
    timer.current = 0;
    const text = JSON.stringify(current.current);
    if (design === null || text === written.current) return;
    written.current = text;
    writeView(design, current.current);
  }, [design]);

  const save = useCallback(
    (change: Partial<IViewMemory>) => {
      const next = { ...current.current, ...change };
      if (JSON.stringify(next) === JSON.stringify(current.current)) return;
      current.current = next;
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(flush, SAVE_MS);
    },
    [flush],
  );

  useEffect(() => {
    window.addEventListener('pagehide', flush);
    return () => {
      window.removeEventListener('pagehide', flush);
      flush();
    };
  }, [flush]);

  return { initial, save };
};
