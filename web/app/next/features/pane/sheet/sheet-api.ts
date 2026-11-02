// What the page and the spreadsheet's worker say to each other, and the worker as promises.
import type { ICellStyle } from '@/next/features/pane/sheet/sheet-core';
import type { IRangeStats } from '@/next/features/pane/sheet/sheet-model';

export const SHEET_FILE = { xlsx: 'xlsx', csv: 'csv' } as const;
export type TSheetFile = (typeof SHEET_FILE)[keyof typeof SHEET_FILE];

export interface IShownCell {
  text: string;
  input: string;
  style: number;
  number: boolean;
  value: number | null;
}

export interface IChangedShown {
  sheet: number;
  row: number;
  column: number;
  cell: IShownCell | null;
}

export type ISheetAsk =
  | { op: 'open'; kind: TSheetFile; bytes: Uint8Array; name: string }
  | { op: 'sheet'; sheet: number }
  | { op: 'rows'; sheet: number; from: number; to: number }
  | { op: 'edit'; sheet: number; edits: { row: number; column: number; input: string }[] }
  | { op: 'stats'; sheet: number; top: number; left: number; bottom: number; right: number }
  | { op: 'merge'; kind: TSheetFile; bytes: Uint8Array; name: string }
  | { op: 'saved' }
  | { op: 'save' };

export type ISheetAnswer =
  | { op: 'open'; sheets: string[]; styles: ICellStyle[] }
  | {
      op: 'sheet';
      rows: number;
      columns: number;
      widths: [number, number][];
      frozenRows: number;
      frozenColumns: number;
    }
  | { op: 'rows'; from: number; rows: (IShownCell | null)[][] }
  | { op: 'edit'; changed: IChangedShown[]; rows: number; columns: number }
  | { op: 'stats'; stats: IRangeStats | null }
  | { op: 'merge'; merged: boolean; sheets: string[]; styles: ICellStyle[] }
  | { op: 'saved' }
  | { op: 'save'; bytes: Uint8Array };

export interface ISheetClient {
  ask: <O extends ISheetAsk['op']>(
    ask: Extract<ISheetAsk, { op: O }>,
  ) => Promise<Extract<ISheetAnswer, { op: O }>>;
  stop: () => void;
}

export const startSheetWorker = (): ISheetClient => {
  const worker = new Worker(new URL('./sheet-worker.ts', import.meta.url), { type: 'module' });
  const waiting = new Map<number, { done: (reply: never) => void; fail: (failure: Error) => void }>();
  let next = 0;
  worker.onmessage = (event: MessageEvent<{ id: number; reply?: ISheetAnswer; failure?: string }>) => {
    const { id, reply, failure } = event.data;
    const asked = waiting.get(id);
    waiting.delete(id);
    if (failure !== undefined) asked?.fail(new Error(failure));
    else asked?.done(reply as never);
  };
  worker.onerror = (event) => {
    for (const asked of waiting.values()) asked.fail(new Error(event.message));
    waiting.clear();
  };
  return {
    ask: (ask) =>
      new Promise((done, fail) => {
        next += 1;
        waiting.set(next, { done: done as never, fail });
        const transfer =
          ask.op === 'open' || ask.op === 'merge'
            ? [(ask as { bytes: Uint8Array }).bytes.buffer as ArrayBuffer]
            : [];
        worker.postMessage({ id: next, ask }, { transfer });
      }),
    stop: () => worker.terminate(),
  };
};
