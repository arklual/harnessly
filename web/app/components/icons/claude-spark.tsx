import { memo, useEffect, useRef, type CSSProperties } from 'react';
import { observeVisibility } from '@/hooks/use-pause-offscreen';
import { isFrameHidden, subscribeFrameVisibility } from '@/lib/frame-visibility';
import { cn } from '@/lib/utils';

export type TSparkState = 'thinking' | 'writing';

const RAYS: [number, number][] = [
  [10, 44],
  [40, 40],
  [56, 46],
  [90, 46],
  [118, 44],
  [140, 40],
  [174, 48],
  [206, 40],
  [234, 44],
  [274, 47],
  [306, 45],
  [348, 44],
];

const EASE = 'cubic-bezier(.45, 0, .55, 1)';

/** Keyframes carry their own easing: as with a CSS animation, it applies to every segment, not to the whole cycle. */
const MOTION: Record<
  TSparkState,
  {
    ray: (i: number) => [Keyframe[], KeyframeAnimationOptions];
    core?: [Keyframe[], KeyframeAnimationOptions];
  }
> = {
  writing: {
    ray: (i) => [
      [{ transform: 'rotate(0deg) scaleX(0.82)', easing: EASE }, { transform: 'rotate(3deg) scaleX(1.02)' }],
      { duration: 900, iterations: Infinity, direction: 'alternate', delay: i * -230 },
    ],
  },
  thinking: {
    ray: (i) => [
      [
        { transform: 'scaleX(0.12)', easing: EASE },
        { transform: 'scaleX(1)', offset: 0.45, easing: EASE },
        { transform: 'scaleX(1)', offset: 0.55, easing: EASE },
        { transform: 'scaleX(0.12)' },
      ],
      { duration: 1600, iterations: Infinity, delay: i * 25 },
    ],
    core: [
      [
        { transform: 'scale(1.35)', easing: EASE },
        { transform: 'scale(1)', easing: EASE },
        { transform: 'scale(1.35)' },
      ],
      { duration: 1600, iterations: Infinity },
    ],
  },
};

/**
 * Claude spark with each ray breathing on its own phase; smooth, so it never reads as a dropped frame.
 * Rays are boxes animated through the Web Animations API: a transform of an HTML box runs on the
 * compositor, where an SVG child's ran on the main thread; and unlike CSS animations these fire no
 * `animationiteration` event, which React listens for at the root, so twelve rays do not wake the page 13 times a
 * second. The spark holds still while it is scrolled out of view, in a machine frame the shell hides, and for reduced
 * motion.
 */
const ClaudeSpark = ({ state = 'thinking', className }: { state?: TSparkState; className?: string }) => {
  const ref = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    const root = ref.current;
    if (
      !root ||
      typeof root.animate !== 'function' ||
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    )
      return;
    const motion = MOTION[state];
    const beams = root.querySelectorAll<HTMLElement>('.tw-spark-beam');
    const core = root.querySelector<HTMLElement>('.tw-spark-core');
    const animations = [...beams].map((beam, i) => beam.animate(...motion.ray(i)));
    if (motion.core && core) animations.push(core.animate(...motion.core));
    let inView = true;
    const sync = () =>
      animations.forEach((animation) => (inView && !isFrameHidden() ? animation.play() : animation.pause()));
    const stopView = observeVisibility(root, (visible) => {
      inView = visible;
      sync();
    });
    const stopFrame = subscribeFrameVisibility(sync);
    return () => {
      stopView();
      stopFrame();
      animations.forEach((animation) => animation.cancel());
    };
  }, [state]);

  return (
    <span ref={ref} className={cn('tw-spark', `tw-spark-${state}`, className)} aria-hidden="true">
      {RAYS.map(([angle, length]) => (
        <span
          key={angle}
          className="tw-spark-ray"
          style={{ '--a': `${angle}deg`, '--l': length } as CSSProperties}
        >
          <span className="tw-spark-beam" />
        </span>
      ))}
      <span className="tw-spark-core" />
    </span>
  );
};

export default memo(ClaudeSpark);
