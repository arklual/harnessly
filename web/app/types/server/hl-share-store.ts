// Shapes of what the runtime answers; the runtime itself is Go.
import type { IShareReport } from '@/lib/hl-share-snapshot';

export type TShareExpiryDays = 7 | 30 | 90;

export type TShareStatus = 'published' | 'revoked' | 'expired';

export interface IShareViewer {
  sub: string;
  at: number;
}

export interface IShareView {
  id: string;
  /** Only a live link has one. */
  url: string | null;
  status: TShareStatus;
  tabId: string;
  title: string;
  version: number;
  publishedAt: number;
  updatedAt: number;
  expiresAt: number;
  /** Latest the link may be extended to. */
  maxExpiresAt: number;
  revokedAt: number | null;
  views: number;
  viewers: IShareViewer[];
  report: IShareReport;
}
