// Shapes of what the runtime answers; the runtime itself is Go.

export type TCheckView = 'desktop' | 'phone';

export type TCheckStatus = 'ok' | 'warning' | 'error' | 'blank';

export interface ICheckShot {
  view: TCheckView;
  status: TCheckStatus;
  /** File name under `.check/`. */
  screenshot: string | null;
  errors: string[];
  failedRequests: string[];
  ms: number;
}

export interface ICheckBoard {
  title: string;
  path: string;
  status: TCheckStatus;
  shots: ICheckShot[];
}

export interface ICheckReport {
  artifact: string;
  startedAt: number;
  finishedAt: number | null;
  done: number;
  total: number;
  boards: ICheckBoard[];
  /** File name of the main board's desktop screenshot: the design's cover. */
  cover: string | null;
  /** Why the check could not run at all (no Chrome on the machine…). */
  failure: string | null;
}
