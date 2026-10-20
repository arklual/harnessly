import { useRef } from 'react';
import usePauseOffscreen from '@/hooks/use-pause-offscreen';
import { cn } from '@/lib/utils';

interface IShimmerTextProps {
  children: string;
  active?: boolean;
  className?: string;
}

/**
 * claude.ai's travelling highlight over a line of text. The highlight is a lighter copy of the text seen through a
 * window: the window slides one way and the copy the other way, both by transform, so the compositor draws every
 * frame. The old background-position gradient repainted the text on the main thread, and a long timeline full of
 * never-finished tool calls kept it at 100%.
 */
const ShimmerText = ({ children, active = true, className }: IShimmerTextProps) => {
  const ref = useRef<HTMLSpanElement>(null);
  usePauseOffscreen(ref, active);
  return (
    <span ref={ref} className={cn(active && 'tw-shimmer', className)}>
      {children}
      {active && (
        <span className="tw-shimmer-band" aria-hidden="true">
          <span className="tw-shimmer-glint">{children}</span>
        </span>
      )}
    </span>
  );
};

export default ShimmerText;
