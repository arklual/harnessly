// The two small pictures of the screen, drawn in SVG: sizes are attributes, never inline styles.
import type { LimitTone } from '@/next/legacy/manage/model';

/** A limit's line: spent, the pace to the month's end (hatched) and the mark where the warning comes. */
export const LimitMeter = ({
  used,
  amount,
  forecast,
  warnPct,
  tone,
  label,
}: {
  used: number;
  amount: number;
  forecast: number | null;
  warnPct: number;
  tone: LimitTone;
  label: string;
}) => {
  const pct = (v: number) => `${Math.min(100, Math.max(0, amount > 0 ? (v / amount) * 100 : 0))}%`;
  const to = forecast !== null && forecast > used ? forecast : used;
  return (
    <svg className="hx-mg-meter" data-tone={tone} width="100%" height="14" role="img" aria-label={label}>
      <defs>
        <pattern
          id="hx-mg-hatch"
          width="6"
          height="6"
          patternUnits="userSpaceOnUse"
          patternTransform="rotate(45)"
        >
          <rect width="3" height="6" className="hx-mg-meter__hatch" />
        </pattern>
      </defs>
      <rect className="hx-mg-meter__track" width="100%" height="8" y="3" rx="4" />
      <rect className="hx-mg-meter__pace" width={pct(to)} height="8" y="3" rx="4" fill="url(#hx-mg-hatch)" />
      <rect className="hx-mg-meter__fill" width={pct(used)} height="8" y="3" rx="4" />
      <rect className="hx-mg-meter__warn" x={pct((amount * warnPct) / 100)} width="2" height="14" />
    </svg>
  );
};

/** Cost per day as bars; the last day is stronger. Nothing to draw (all zero) is the caller's empty state. */
export const DayBars = ({ days, label }: { days: { day: string; cost: number }[]; label: string }) => {
  const max = Math.max(...days.map((d) => d.cost), 0);
  const n = Math.max(1, days.length);
  const step = 100 / n;
  return (
    <svg className="hx-mg-bars" width="100%" height="120" role="img" aria-label={label}>
      {days.map((d, i) => {
        const h = max > 0 ? Math.max(d.cost > 0 ? 3 : 0, (d.cost / max) * 100) : 0;
        return (
          <rect
            key={d.day}
            className={i === n - 1 ? 'hx-mg-bars__last' : 'hx-mg-bars__bar'}
            x={`${i * step + step * 0.15}%`}
            width={`${step * 0.7}%`}
            y={`${100 - h}%`}
            height={`${h}%`}
            rx="2"
          >
            <title>{d.day}</title>
          </rect>
        );
      })}
    </svg>
  );
};
