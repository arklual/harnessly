import { IconButton } from '@kit';
import { X } from 'lucide-react';
import { useEffect } from 'react';

import { errorStore } from '../stores/runtime';
import { useStore } from '../stores/store';

/** The last error of an action, shown for a while at the bottom. */
export function Toasts() {
  const error = useStore(errorStore);
  useEffect(() => {
    if (!error) return;
    const t = setTimeout(() => errorStore.set(null), 7000);
    return () => clearTimeout(t);
  }, [error]);
  if (!error) return null;
  return (
    <div className="toast" role="alert">
      <span>{error}</span>
      <IconButton label="Dismiss" size="xs" onClick={() => errorStore.set(null)}>
        <X size={14} />
      </IconButton>
    </div>
  );
}
