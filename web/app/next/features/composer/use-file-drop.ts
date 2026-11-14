// Files dragged anywhere over the window go to a field (the design's dragDepth): enter and leave fire for every element
// crossed, so the depth says whether the drag is still over the page. Answers whether files are being carried now.
//
// a screen may hold two fields of one draft at once (Sinopia's folded field and its open chat), each
// listening on the window. A drop is taken once, by a field the person can see; the same file twice in it is one.
import { useEffect, useEffectEvent, useState, type RefObject } from 'react';
import { distinctFiles } from '@/next/features/composer/distinct-files';

/** The drops a field has taken. */
const dropped = new WeakSet<Event>();

/** A field hidden from the person (a folded island, a closed column) leaves the drop to the one in sight. */
const inSight = (root: HTMLElement | null | undefined): boolean => {
  if (!root) return true;
  if (typeof root.checkVisibility !== 'function') return true;
  return root.checkVisibility({ opacityProperty: true, visibilityProperty: true });
};

export const useFileDrop = (
  onFiles: (files: File[]) => void,
  root?: RefObject<HTMLElement | null>,
): boolean => {
  const [dropping, setDropping] = useState(false);
  // The listeners stay across a change of the caller's function: a new set would lose the depth in mid-drag.
  const take = useEffectEvent(onFiles);
  const visible = useEffectEvent(() => inSight(root?.current));
  useEffect(() => {
    let depth = 0;
    const carries = (event: DragEvent) => !!event.dataTransfer?.types.includes('Files');
    const enter = (event: DragEvent) => {
      if (!carries(event)) return;
      depth += 1;
      setDropping(true);
    };
    const over = (event: DragEvent) => {
      if (carries(event)) event.preventDefault();
    };
    const leave = (event: DragEvent) => {
      if (!carries(event)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) setDropping(false);
    };
    const drop = (event: DragEvent) => {
      if (!carries(event)) return;
      event.preventDefault();
      depth = 0;
      setDropping(false);
      if (dropped.has(event) || !visible()) return;
      dropped.add(event);
      void distinctFiles([...(event.dataTransfer?.files ?? [])]).then(take);
    };
    window.addEventListener('dragenter', enter);
    window.addEventListener('dragover', over);
    window.addEventListener('dragleave', leave);
    window.addEventListener('drop', drop);
    return () => {
      window.removeEventListener('dragenter', enter);
      window.removeEventListener('dragover', over);
      window.removeEventListener('dragleave', leave);
      window.removeEventListener('drop', drop);
    };
  }, []);
  return dropping;
};
