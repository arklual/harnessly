// What the person is told while a design opens. Three steps, said calmly in one line: the machine opens the
// page («Открываю макет»); the page's document has arrived and its runtime pulls files («Загружаю ресурсы», known from
// the bridge's progress); and, when that goes on longer than usual, that it is still going («Макет большой»). A page
// that never loads is given up after STALL_MS: a failure with «Повторить», not a wait without end.
import { DOC_STATE, type TDocState } from '@/next/features/designly/page-bridge';

export const LOAD_STAGE = { opening: 'opening', resources: 'resources', slow: 'slow' } as const;
export type TLoadStage = (typeof LOAD_STAGE)[keyof typeof LOAD_STAGE];

/** From this long a load is said to be longer than usual. */
export const SLOW_MS = 8_000;
/** From this long a page that has not loaded is a failure. */
export const STALL_MS = 60_000;

export interface ILoadProgress {
  state: TDocState;
  resources: number;
}

/** `slow`: the load has gone on for SLOW_MS. */
export const loadStageOf = (progress: ILoadProgress | null, slow: boolean): TLoadStage => {
  if (slow) return LOAD_STAGE.slow;
  if (progress && progress.state !== DOC_STATE.complete) return LOAD_STAGE.resources;
  return LOAD_STAGE.opening;
};

/** How far the page is, in files taken; null while nothing was said. */
export const filesTaken = (progress: ILoadProgress | null): number | null =>
  progress && progress.resources > 0 ? progress.resources : null;
