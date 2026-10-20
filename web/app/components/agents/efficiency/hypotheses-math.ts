import type { IEffHypothesis } from '@/types/efficiency';

export interface IHypGroup {
  group: string;
  items: IEffHypothesis[];
}

/** Rows keep the backend's order; a group is a run of rows with the same title. */
export const groupHypotheses = (list: IEffHypothesis[]): IHypGroup[] =>
  list.reduce<IHypGroup[]>((acc, h) => {
    const last = acc[acc.length - 1];
    if (last && last.group === h.group) last.items.push(h);
    else acc.push({ group: h.group, items: [h] });
    return acc;
  }, []);

export interface IStateCount {
  state: string;
  count: number;
}

export const stateCounts = (list: IEffHypothesis[]): IStateCount[] => {
  const counts = new Map<string, number>();
  for (const h of list) counts.set(h.state, (counts.get(h.state) ?? 0) + 1);
  return [...counts.entries()].map(([state, count]) => ({ state, count }));
};

export interface IBullet {
  /** Positions in 0–100: where the September base, the target and the current point sit on the line. */
  base: number;
  target: number;
  current: number | null;
  zoneFrom: number;
  zoneTo: number;
  reached: boolean | null;
}

/** null when the hypothesis has no base or target yet: the row says "measured later" instead of drawing a made-up line. */
export const bullet = (h: Pick<IEffHypothesis, 'base' | 'target' | 'current' | 'better'>): IBullet | null => {
  if (h.base === null || h.target === null) return null;
  const values = [h.base, h.target, ...(h.current === null ? [] : [h.current])];
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const pad = (hi - lo || Math.abs(hi) || 1) * 0.08;
  const pos = (v: number) => ((v - (lo - pad)) / (hi - lo + 2 * pad)) * 100;
  const target = pos(h.target);
  const lowerIsBetter = h.better === 'down';
  let reached: boolean | null = null;
  if (h.current !== null) reached = lowerIsBetter ? h.current <= h.target : h.current >= h.target;
  return {
    base: pos(h.base),
    target,
    current: h.current === null ? null : pos(h.current),
    zoneFrom: lowerIsBetter ? 0 : target,
    zoneTo: lowerIsBetter ? target : 100,
    reached,
  };
};

/** Share of the way from base to target already covered, clamped to 0–1. */
export const progress = (h: Pick<IEffHypothesis, 'base' | 'target' | 'current'>): number | null => {
  if (h.base === null || h.target === null || h.current === null || h.base === h.target) return null;
  return Math.max(0, Math.min(1, (h.current - h.base) / (h.target - h.base)));
};
