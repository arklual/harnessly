// A skeleton that gives way to its content on the same spot: both are in the same place for the length of a
// crossfade, the skeleton fading out over the content fading in, so nothing jumps and nothing enters twice. The entrance
// of the content's own (a card sliding up, .hx-enter) belongs to a content that came with no skeleton before it; after a
// skeleton it does not play again — the features' css switches theirs off under .hx-swap[data-phase='swapping'|'swapped'].
import { useEffect, useState, type ReactNode } from 'react';
import { useDelayed } from '@/next/ui/skeleton';

/** The crossfade, as long as hx-swap-in / hx-swap-out in ui.css. */
export const SWAP_MS = 240;

export const SWAP_PHASE = {
  loading: 'loading',
  /** The content came with no skeleton drawn before it: it plays its own entrance. */
  direct: 'direct',
  swapping: 'swapping',
  swapped: 'swapped',
} as const;
type TSwapPhase = (typeof SWAP_PHASE)[keyof typeof SWAP_PHASE];

export interface ISwapProps {
  /** The data is there: the content shows. false is loading (the skeleton shows after the usual delay). */
  ready: boolean;
  /** The content is in place while it loads, unseen (a frame that has to load where it will stand): it appears with the crossfade. */
  mounted?: boolean;
  /** Bars of the same size as the content's. */
  skeleton: ReactNode;
  children: ReactNode;
  className?: string;
  /** The content's own box: a flex column where the content's children are laid out so. */
  contentClassName?: string;
}

const Swap = ({ ready, mounted = false, skeleton, children, className, contentClassName }: ISwapProps) => {
  const visible = useDelayed(!ready);
  const [phase, setPhase] = useState<TSwapPhase>(ready ? SWAP_PHASE.direct : SWAP_PHASE.loading);
  const [seen, setSeen] = useState(false);

  // State follows the props while rendering, not in an effect: the frame that brings the content never shows both
  // without the phase.
  if (visible && !seen) setSeen(true);
  if (ready && phase === SWAP_PHASE.loading) setPhase(seen ? SWAP_PHASE.swapping : SWAP_PHASE.direct);
  if (!ready && phase !== SWAP_PHASE.loading) {
    setPhase(SWAP_PHASE.loading);
    setSeen(false);
  }

  useEffect(() => {
    if (phase !== SWAP_PHASE.swapping) return;
    const timer = setTimeout(() => setPhase(SWAP_PHASE.swapped), SWAP_MS);
    return () => clearTimeout(timer);
  }, [phase]);

  const showSkeleton = phase === SWAP_PHASE.swapping || (phase === SWAP_PHASE.loading && visible);
  return (
    <div className={className ? `hx-swap ${className}` : 'hx-swap'} data-phase={phase}>
      {(ready || mounted) && (
        <div
          className={contentClassName ? `hx-swap__content ${contentClassName}` : 'hx-swap__content'}
          data-held={!ready}
        >
          {children}
        </div>
      )}
      {showSkeleton && (
        <div className="hx-swap__sk" aria-hidden="true">
          {skeleton}
        </div>
      )}
    </div>
  );
};

export default Swap;
