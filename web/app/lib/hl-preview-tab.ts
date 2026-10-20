import { t } from '@/lib/i18n';
import { toast } from 'sonner';
import { currentMachineId } from '@/lib/app-path';
import { openPreview } from '@/lib/hl-preview-client';
import { claimPreviewSlot } from '@/lib/hl-preview-store';

/** A dev server of the conversation's machine, as a link names it. */
export interface ILocalServer {
  port: number;
  path: string;
}

/**
 * A dev-server link of the chat in a tab of its own (⌘/Ctrl/Shift or middle click). The person's own `localhost`
 * is not the machine the server runs on, so the tab is a slot of the preview, as with "Open in new tab" of the
 * panel: the blank tab is opened inside the click (or the browser would block it) and sent to the one-time enter address.
 */
export const openLocalServerInTab = async ({ port, path }: ILocalServer): Promise<void> => {
  const popup = window.open('about:blank', '_blank');
  if (!popup) {
    toast.error(t('lib.hlPreviewTab.popupBlocked'));
    return;
  }
  popup.opener = null;
  try {
    const slot = claimPreviewSlot(currentMachineId() ?? '', { kind: 'port', port });
    const preview = await openPreview({ kind: 'port', port, path }, slot);
    popup.location.href = preview.enterUrl;
  } catch (error) {
    popup.close();
    toast.error(error instanceof Error ? error.message : t('lib.hlPreviewTab.openFailed'));
  }
};
