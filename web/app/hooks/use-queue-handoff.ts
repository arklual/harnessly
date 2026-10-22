import { useEffect, useState } from 'react';
import { onQueueHandoff } from '@/hooks/use-prompt-queue';
import { uploadUrlOfPath } from '@/lib/attachment-media';
import { scopedAppPath } from '@/lib/app-path';
import { pendingFromHandoff } from '@/lib/hl-queue-handoff';
import type { IPendingMessageOptions } from '@/types/timeline';

/**
 * A message this page queued shows above the composer; when the runtime takes it out for the agent it goes
 * into the timeline as a pending row, replaced by the transcript's record as any sent message is. A message that comes
 * back to the queue after a failed hand-over loses its row again.
 */
const useQueueHandoff = (
  tabId: string | null | undefined,
  machineId: string | undefined,
  addPending: ((text: string, options?: IPendingMessageOptions) => string) | undefined,
  removePending: ((id: string) => void) | undefined,
): void => {
  const [rows] = useState(() => new Map<string, string>());

  useEffect(() => {
    if (!tabId || !addPending) return;
    const imageUrl = (path: string) => {
      const url = uploadUrlOfPath(path);
      return url ? scopedAppPath(url, machineId) : null;
    };
    return onQueueHandoff(tabId, (event) => {
      if (event.kind === 'handoff') {
        const { text, options } = pendingFromHandoff(event.item, imageUrl);
        rows.set(event.item.id, addPending(text, options));
        return;
      }
      const row = rows.get(event.id);
      rows.delete(event.id);
      if (row) removePending?.(row);
    });
  }, [tabId, machineId, addPending, removePending, rows]);
};

export default useQueueHandoff;
