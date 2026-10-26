import { targetLabel } from '@/lib/hl-preview-client';
import type { TPreviewState } from '@/lib/hl-preview-store';

// Apart from browser-tab, so the pane's tab titles do not bring the browser tab's view with the page.

/** The address the field shows: host (or port) and the page path. */
export const shownAddress = (state: TPreviewState): string => {
  if (state.kind !== 'open' && state.kind !== 'opening' && state.kind !== 'taken') return '';
  const path = state.kind === 'opening' ? '/' : state.path;
  return `${targetLabel(state.target)}${path === '/' ? '' : path}`;
};
