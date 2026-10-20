import { useEffect, useEffectEvent } from 'react';
import { pushBackLayer } from '@/lib/hl-back-layers';

/** While `open`, the phone's Back closes this panel. */
const useBackLayer = (open: boolean, close: () => void, enabled = true) => {
  const onBack = useEffectEvent(close);
  useEffect(() => {
    if (!open || !enabled) return;
    return pushBackLayer(() => onBack());
  }, [open, enabled]);
};

export default useBackLayer;
