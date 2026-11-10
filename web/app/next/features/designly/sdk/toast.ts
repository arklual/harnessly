// Sinopia's toasts: the editor shows a short message through its SDK (hl.ui.toast), as an app does, not
// through the host's toast module. The same call the editor made before: words, a tone, a second line.
import type { IToastOptions } from '@/next/features/app-views/bridge';
import { sinopiaUi } from '@/next/features/app-views/sinopia/host';

export const toast = (text: string, options: IToastOptions = {}): void => {
  void sinopiaUi()
    .ui.toast(text, options)
    .catch(() => undefined);
};
