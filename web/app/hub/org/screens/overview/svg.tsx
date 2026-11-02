// The drawings of Обзор. Plain SVG: every size is an attribute, nothing is styled inline.
import type { ReactNode } from 'react';

const rad = (deg: number) => (deg * Math.PI) / 180;

const arc = (cx: number, cy: number, r: number, from: number, to: number): string => {
  const x = (a: number) => cx + r * Math.cos(rad(a));
  const y = (a: number) => cy + r * Math.sin(rad(a));
  return `M ${x(from).toFixed(2)} ${y(from).toFixed(2)} A ${r} ${r} 0 ${to - from > 180 ? 1 : 0} 1 ${x(to).toFixed(2)} ${y(to).toFixed(2)}`;
};

export interface GaugePart {
  id: string;
  value: number;
  /** 0..3: the series colour. */
  tone: number;
}

/** A ring open at the bottom, cut into rounded pieces by value (the board's «Расход по командам»). */
export const Gauge = ({ parts, children }: { parts: GaugePart[]; children: ReactNode }) => {
  const total = parts.reduce((s, p) => s + p.value, 0) || 1;
  const start = 150;
  const sweep = 240;
  const gap = 6;
  const ends = parts.reduce<number[]>(
    (acc, p) => [...acc, (acc[acc.length - 1] ?? start) + (p.value / total) * sweep],
    [],
  );
  return (
    <div className="ov-gauge">
      <svg viewBox="0 0 200 150" width="100%" aria-hidden="true">
        {parts.map((p, i) => {
          const from = i === 0 ? start : (ends[i - 1] ?? start);
          const to = ends[i] ?? from;
          return (
            <path
              key={p.id}
              className="ov-gauge__seg"
              data-tone={p.tone}
              d={arc(100, 100, 80, from + gap / 2, Math.max(from + gap / 2 + 1, to - gap / 2))}
              fill="none"
              strokeWidth="11"
              strokeLinecap="round"
            />
          );
        })}
      </svg>
      <div className="ov-gauge__mid">{children}</div>
    </div>
  );
};

/** A row with a coloured fill behind it, as wide as the share (the board's bar list). */
export const FillRow = ({
  pct,
  tone,
  children,
}: {
  pct: number;
  tone: 'ok' | 'warn' | 'bad' | 'plain';
  children: ReactNode;
}) => (
  <div className="ov-fill" data-tone={tone}>
    <svg className="ov-fill__bg" width="100%" height="100%" preserveAspectRatio="none" aria-hidden="true">
      <rect width={`${Math.min(100, Math.max(2, pct))}%`} height="100%" rx="10" />
    </svg>
    <div className="ov-fill__in">{children}</div>
  </div>
);

/** Spent against a limit: the bar of the spent part and a tick at the limit. Values are on one common scale. */
export const LimitBar = ({
  spent,
  limit,
  scale,
  tone,
}: {
  spent: number;
  limit: number | null;
  scale: number;
  tone: 'ok' | 'bad';
}) => {
  const w = (v: number) => `${Math.min(100, (v / scale) * 100)}%`;
  return (
    <svg className="ov-limitbar" data-tone={tone} width="100%" height="14" aria-hidden="true">
      <rect className="ov-limitbar__fill" width={w(spent)} y="3" height="8" rx="4" />
      {limit != null && <rect className="ov-limitbar__tick" x={w(limit)} width="2" height="14" rx="1" />}
    </svg>
  );
};
