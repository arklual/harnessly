// Files dragged anywhere over the window go to a field (the design's dragDepth): enter and leave fire for every element
// crossed, so the depth says whether the drag is still over the page. Answers whether files are being carried now.
import { useEffect, useEffectEvent, useState } from 'react';

export const useFileDrop = (onFiles: (files: File[]) => void): boolean => {
  const [dropping, setDropping] = useState(false);
  // The listeners stay across a change of the caller's function: a new set would lose the depth in mid-drag.
  const take = useEffectEvent(onFiles);
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
      take([...(event.dataTransfer?.files ?? [])]);
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
