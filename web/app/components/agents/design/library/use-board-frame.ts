import { useEffect, useEffectEvent } from 'react';
import {
  asksForAnotherPage,
  shownGateKey,
  targetGateKey,
  usePreviewOpener,
} from '@/components/agents/inspector/preview-island';
import { parsePreviewTarget } from '@/lib/hl-preview-client';
import { readPreview, usePreviewState } from '@/lib/hl-preview-store';

/** One preview of the centre section: the key is not a conversation's tab, so its comments wait here, not in a composer. */
export const PREVIEW_KEY = 'design-library';

/**
 * The preview of the centre section shows the board at `address` of `machine` (the machine the project lives
 * on): it opens when the board on screen is another one, or the same address of another machine.
 */
export const useBoardFrame = (address: string | null, machine: string | undefined) => {
  const [{ state }, patch] = usePreviewState(PREVIEW_KEY);
  const { open } = usePreviewOpener(PREVIEW_KEY, machine);
  const show = useEffectEvent((to: string) => {
    let target;
    try {
      target = parsePreviewTarget(to);
    } catch {
      return;
    }
    if (!target) return;
    const shown = readPreview(PREVIEW_KEY).state;
    const otherMachine = shown.kind === 'open' && shown.preview.machineId !== machine;
    if (otherMachine || targetGateKey(target) !== shownGateKey(shown) || asksForAnotherPage(target, shown))
      void open(target, []);
  });
  useEffect(() => {
    if (address) show(address);
  }, [address, machine]);
  return { state, patch, open };
};
