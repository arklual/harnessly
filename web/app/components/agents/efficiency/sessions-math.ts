import type { IEffSession } from '@/types/efficiency';

export type TSessionSort = 'cost' | 'mrs' | 'date';

export const SESSION_LIMIT_CTX = 250_000;

export const sortSessions = (sessions: IEffSession[], by: TSessionSort): IEffSession[] => {
  const key = {
    cost: (s: IEffSession) => s.costUsd,
    mrs: (s: IEffSession) => s.mrs,
    date: (s: IEffSession) => Date.parse(s.endedAt ?? s.startedAt ?? '') || 0,
  }[by];
  return [...sessions].sort((a, b) => key(b) - key(a) || b.costUsd - a.costUsd);
};

/** The contract promises a plain list of contexts; the first cluster build sends [hours-from-start, context] pairs. Both are read. */
export const cardioPoints = (cardio: unknown[]): { x: number; ctx: number }[] =>
  cardio.flatMap((p, i) => {
    if (typeof p === 'number') return [{ x: i, ctx: p }];
    if (Array.isArray(p) && typeof p[0] === 'number' && typeof p[1] === 'number')
      return [{ x: p[0], ctx: p[1] }];
    return [];
  });

export interface ICardioPath {
  line: string;
  /** The part of the curve above the 250K line, closed against the line: what a fresh session would have saved. */
  over: string;
  limitY: number;
}

export const cardioPath = (
  cardio: unknown[],
  width: number,
  height: number,
  maxCtx = 1_000_000,
): ICardioPath | null => {
  const points = cardioPoints(cardio);
  if (points.length < 2) return null;
  const xMax = Math.max(points[points.length - 1].x, 1e-9);
  const x = (v: number) => (v / xMax) * width;
  const y = (ctx: number) => height - (Math.min(ctx, maxCtx) / maxCtx) * height;
  const limitY = y(SESSION_LIMIT_CTX);
  const line = points.map((p, i) => `${i ? 'L' : 'M'}${x(p.x).toFixed(1)} ${y(p.ctx).toFixed(1)}`).join(' ');
  const top = points.map((p) => `L${x(p.x).toFixed(1)} ${Math.min(limitY, y(p.ctx)).toFixed(1)}`).join(' ');
  const over = `M${x(points[0].x).toFixed(1)} ${limitY.toFixed(1)} ${top} L${x(xMax).toFixed(1)} ${limitY.toFixed(1)} Z`;
  return { line, over, limitY };
};

export const sessionDuration = (s: Pick<IEffSession, 'startedAt' | 'endedAt'>): number | null => {
  if (!s.startedAt || !s.endedAt) return null;
  const ms = Date.parse(s.endedAt) - Date.parse(s.startedAt);
  return Number.isFinite(ms) && ms >= 0 ? ms / 3_600_000 : null;
};
