// Shapes of what the runtime answers; the runtime itself is Go.
import type { IShareReport } from '@/lib/hl-share-snapshot';

export interface IShareDraftView {
  id: string;
  title: string;
  createdAt: number;
  expiresAt: number;
  report: IShareReport;
}
