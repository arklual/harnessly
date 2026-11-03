import { barRects } from './geometry';

/** The bars of a tile (`svg.sx-bars`): the last 14 values, the newest one marked. */
export const Bars = ({ values }: { values: readonly number[] }) => (
  <svg className="sx-bars" viewBox="0 0 100 40" preserveAspectRatio="none" aria-hidden="true">
    {barRects(values).map((r, i) => (
      <rect
        key={i}
        x={r.x.toFixed(2)}
        y={r.y.toFixed(2)}
        width={r.width}
        height={r.height.toFixed(2)}
        rx="1.2"
        data-recent={r.recent ? 'true' : undefined}
      />
    ))}
  </svg>
);
