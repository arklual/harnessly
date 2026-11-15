// The model pill's outline as the limit's meter: a stroke along the pill's contour, from the top centre
// clockwise, as long as the share of the five-hour window already spent; its colour is the window's level (the theme's
// accent, warning, danger). The pill's own border stays under it as the track. The contour follows the pill's size (a
// name that changes, a phone's lower field), so it is drawn from the measured box, not from a fixed shape.
import { useLayoutEffect, useRef, useState } from 'react';
import type { Level } from '@/next/features/limits/model';

/** The stroke's centre line sits this far inside the pill's padding box, px: half the stroke, on the border's inner edge. */
const INSET = 1;

/** A rounded rectangle of w×h (radius half the height) from the top centre clockwise. */
export const pillPath = (w: number, h: number): string => {
  const x0 = INSET;
  const y0 = INSET;
  const x1 = w - INSET;
  const y1 = h - INSET;
  const r = Math.max(0, Math.min((y1 - y0) / 2, (x1 - x0) / 2));
  const cx = w / 2;
  return [
    `M ${cx} ${y0}`,
    `H ${x1 - r}`,
    `A ${r} ${r} 0 0 1 ${x1} ${y0 + r}`,
    `V ${y1 - r}`,
    `A ${r} ${r} 0 0 1 ${x1 - r} ${y1}`,
    `H ${x0 + r}`,
    `A ${r} ${r} 0 0 1 ${x0} ${y1 - r}`,
    `V ${y0 + r}`,
    `A ${r} ${r} 0 0 1 ${x0 + r} ${y0}`,
    'Z',
  ].join(' ');
};

interface IPillRingProps {
  /** The pill the ring goes round. */
  host: HTMLElement | null;
  /** Spent, whole per cent. */
  used: number;
  level: Level;
}

const PillRing = ({ host, used, level }: IPillRingProps) => {
  const svgRef = useRef<SVGSVGElement>(null);
  const [box, setBox] = useState<{ w: number; h: number } | null>(null);

  useLayoutEffect(() => {
    if (!host) return;
    const measure = () => {
      const w = host.clientWidth;
      const h = host.clientHeight;
      setBox((prev) => (prev && prev.w === w && prev.h === h ? prev : { w, h }));
    };
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(host);
    return () => observer.disconnect();
  }, [host]);

  useLayoutEffect(() => {
    svgRef.current?.style.setProperty('--hx-ring-used', String(Math.max(0, Math.min(100, used))));
  }, [used]);

  return (
    <svg ref={svgRef} className="hx-model__ring" data-level={level} aria-hidden="true">
      {box && box.w > 0 && <path d={pillPath(box.w, box.h)} pathLength={100} />}
    </svg>
  );
};

export default PillRing;
