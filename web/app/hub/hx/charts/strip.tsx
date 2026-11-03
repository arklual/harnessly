import type { CSSProperties } from 'react';

export interface StripPart {
  /** The weight of the part; the strip splits its width in these proportions. */
  w: number;
  /** A colour; absent: the muted remainder. */
  color?: string;
}

/** The segmented strip of a tile (`span.sx-strip`): coloured parts and a muted rest. */
export const Strip = ({ parts }: { parts: readonly StripPart[] }) => (
  <span className="sx-strip" aria-hidden="true">
    {parts
      .filter((p) => p.w > 0)
      .map((p, i) => (
        <span
          key={i}
          className={p.color ? 'sx-strip__part' : 'sx-strip__part sx-strip__part--muted'}
          style={{ '--sx-w': p.w, ...(p.color ? { '--sx-c': p.color } : {}) } as CSSProperties}
        />
      ))}
  </span>
);
