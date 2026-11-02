// The open spreadsheet's life: the worker, the rows it holds for the page, an edit, and the save that follows it. An edit
// shows when the worker has taken it, and goes to disk a moment after the last one.
import { useCallback, useEffect, useRef, useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import type { ICellStyle } from '@/next/features/pane/sheet/sheet-core';
import {
  startSheetWorker,
  type ISheetClient,
  type IShownCell,
  type TSheetFile,
} from '@/next/features/pane/sheet/sheet-api';
import { FileConflict, fileRevision, saveFileBytes } from '@/next/features/pane/sheet/file-bytes';
import { useFileLook } from '@/next/features/pane/use-file-changes';
import { SAVE, type TSave } from '@/next/features/pane/file-save-state';
import { CHUNK_ROWS, type IRange, type IRangeStats } from '@/next/features/pane/sheet/sheet-model';

const SAVE_PAUSE_MS = 700;
/** A file written again and again under the save: past this the person hears of it. */
const MAX_MERGES = 3;

export interface ISheetShape {
  rows: number;
  columns: number;
  widths: ReadonlyMap<number, number>;
  frozenRows: number;
  frozenColumns: number;
}

interface IUseSheetFile {
  machine: MachineId;
  path: string;
  src: string;
  name: string;
  kind: TSheetFile;
}

export const useSheetFile = ({ machine, path, src, name, kind }: IUseSheetFile) => {
  const client = useRef<ISheetClient | null>(null);
  const chunks = useRef(new Map<string, (IShownCell | null)[][]>());
  const asked = useRef(new Set<string>());
  const revision = useRef<string | null>(null);
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const saving = useRef(false);
  const again = useRef(false);
  const writeOutRef = useRef<(force: boolean) => Promise<void>>(() => Promise.resolve());
  const [sheets, setSheets] = useState<readonly string[]>([]);
  const [styles, setStyles] = useState<readonly ICellStyle[]>([]);
  const [sheet, setSheet] = useState(0);
  const [shape, setShape] = useState<ISheetShape | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const [save, setSave] = useState<TSave>(SAVE.clean);
  const [version, setVersion] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const [shownRevision, setShownRevision] = useState<string | null>(null);
  const sheetRef = useRef(0);

  const openSheet = useCallback(async (at: number) => {
    const answer = await client.current?.ask({ op: 'sheet', sheet: at });
    if (!answer) return;
    chunks.current.clear();
    asked.current.clear();
    sheetRef.current = at;
    setSheet(at);
    setShape({
      rows: answer.rows,
      columns: answer.columns,
      widths: new Map(answer.widths),
      frozenRows: answer.frozenRows,
      frozenColumns: answer.frozenColumns,
    });
  }, []);

  useEffect(() => {
    const abort = new AbortController();
    const started = startSheetWorker();
    client.current = started;
    void (async () => {
      revision.current = await fileRevision(machine, path, abort.signal);
      setShownRevision(revision.current);
      const res = await fetch(src, { credentials: 'same-origin', signal: abort.signal, cache: 'no-store' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const bytes = new Uint8Array(await res.arrayBuffer());
      const opened = await started.ask({ op: 'open', kind, bytes, name });
      if (abort.signal.aborted) return;
      setSheets(opened.sheets);
      setStyles(opened.styles);
      setSave(SAVE.clean);
      await openSheet(0);
    })().catch((failure: Error) => {
      if (!abort.signal.aborted) setFailed(failure.message);
    });
    return () => {
      abort.abort();
      // An edit still waiting for its pause is saved on the way out: closing the tab is no reason to lose it.
      if (saveTimer.current !== null) {
        clearTimeout(saveTimer.current);
        saveTimer.current = null;
        void writeOutRef.current(false).finally(() => started.stop());
      } else if (saving.current) {
        const waitDone = setInterval(() => {
          if (saving.current) return;
          clearInterval(waitDone);
          started.stop();
        }, 100);
      } else started.stop();
    };
  }, [machine, path, src, name, kind, attempt, openSheet]);

  /** A row's cells if its chunk is here; the chunk is asked for when it is not. */
  const rowOf = (row: number): readonly (IShownCell | null)[] | null => {
    if (shape === null) return null;
    const from = row - (row % CHUNK_ROWS);
    const key = `${sheet}:${from}`;
    const chunk = chunks.current.get(key);
    if (chunk) return chunk[row - from] ?? [];
    if (!asked.current.has(key) && client.current) {
      asked.current.add(key);
      void client.current.ask({ op: 'rows', sheet, from, to: from + CHUNK_ROWS }).then(
        (answer) => {
          chunks.current.set(key, answer.rows);
          setVersion((n) => n + 1);
        },
        () => asked.current.delete(key),
      );
    }
    return null;
  };

  // The rows on screen are read again, the sheet and what is selected stay.
  const showAgain = useCallback(
    async (names: readonly string[], cellStyles: readonly ICellStyle[]) => {
      setSheets(names);
      setStyles(cellStyles);
      await openSheet(Math.min(sheetRef.current, Math.max(0, names.length - 1)));
      setVersion((n) => n + 1);
    },
    [openSheet],
  );

  const readDisk = useCallback(async () => {
    const res = await fetch(src, { credentials: 'same-origin', cache: 'no-store' });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return new Uint8Array(await res.arrayBuffer());
  }, [src]);

  // One save at a time: a second one, sent over the revision the first is about to replace, would refuse itself as a conflict.
  // A file that changed under the edits takes them on top, cell by cell, and goes again: the person chooses only when an edit
  // has nowhere left to go.
  const writeOut = useCallback(
    async (force: boolean) => {
      const worker = client.current;
      if (!worker) return;
      if (saving.current) {
        again.current = true;
        return;
      }
      saving.current = true;
      setSave(SAVE.saving);
      try {
        let over = force ? null : revision.current;
        let merges = 0;
        do {
          again.current = false;
          const { bytes } = await worker.ask({ op: 'save' });
          try {
            revision.current = await saveFileBytes(machine, path, bytes, over);
          } catch (failure) {
            if (!(failure instanceof FileConflict) || merges >= MAX_MERGES) throw failure;
            merges += 1;
            const rev = await fileRevision(machine, path);
            const merged = await worker.ask({ op: 'merge', kind, bytes: await readDisk(), name });
            if (!merged.merged) throw failure;
            revision.current = rev;
            over = rev;
            again.current = true;
            await showAgain(merged.sheets, merged.styles);
            continue;
          }
          await worker.ask({ op: 'saved' });
          over = revision.current;
        } while (again.current);
        setShownRevision(revision.current);
        setSave(SAVE.saved);
      } catch (failure) {
        setSave(failure instanceof FileConflict ? SAVE.conflict : SAVE.failed);
      } finally {
        saving.current = false;
      }
    },
    [machine, path, kind, name, readDisk, showAgain],
  );

  // The file on the disk moved on (the agent wrote it): with no edit waiting it is read again in place.
  const lookAtDisk = async () => {
    const worker = client.current;
    if (!worker || shape === null || saving.current || saveTimer.current !== null || save === SAVE.conflict)
      return;
    const rev = await fileRevision(machine, path).catch(() => null);
    if (rev === null || rev === revision.current || saving.current || saveTimer.current !== null) return;
    const opened = await worker.ask({ op: 'open', kind, bytes: await readDisk(), name });
    revision.current = rev;
    setShownRevision(rev);
    chunks.current.clear();
    asked.current.clear();
    await showAgain(opened.sheets, opened.styles);
  };
  useFileLook(() => void lookAtDisk().catch(() => undefined), shape !== null);

  useEffect(() => {
    writeOutRef.current = writeOut;
  }, [writeOut]);

  const edit = useCallback(
    async (edits: { row: number; column: number; input: string }[]) => {
      const worker = client.current;
      if (!worker) return;
      const answer = await worker.ask({ op: 'edit', sheet, edits });
      for (const change of answer.changed) {
        if (change.sheet !== sheet) continue;
        const from = change.row - (change.row % CHUNK_ROWS);
        const key = `${sheet}:${from}`;
        // A chunk not here yet comes with the change in it, when it is asked for.
        const chunk = chunks.current.get(key);
        if (chunk) (chunk[change.row - from] ??= [])[change.column] = change.cell;
      }
      setShape((before) => before && { ...before, rows: answer.rows, columns: answer.columns });
      setVersion((n) => n + 1);
      setSave(SAVE.dirty);
      if (saveTimer.current !== null) clearTimeout(saveTimer.current);
      saveTimer.current = setTimeout(() => {
        saveTimer.current = null;
        void writeOut(false);
      }, SAVE_PAUSE_MS);
    },
    [sheet, writeOut],
  );

  /** The cells of these rows, once they are all here. */
  const rowsOf = useCallback(
    async (top: number, bottom: number): Promise<(IShownCell | null)[][]> => {
      const answer = await client.current?.ask({ op: 'rows', sheet, from: top, to: bottom + 1 });
      return answer?.rows ?? [];
    },
    [sheet],
  );

  const statsOf = useCallback(
    async (range: IRange): Promise<IRangeStats | null> => {
      const answer = await client.current?.ask({ op: 'stats', sheet, ...range });
      return answer?.stats ?? null;
    },
    [sheet],
  );

  return {
    revision: shownRevision,
    statsOf,
    sheets,
    styles,
    sheet,
    shape,
    failed,
    save,
    version,
    rowOf,
    rowsOf,
    edit,
    openSheet,
    overwrite: () => void writeOut(true),
    reload: () => {
      setFailed(null);
      setShape(null);
      chunks.current.clear();
      asked.current.clear();
      setAttempt((n) => n + 1);
    },
  };
};
