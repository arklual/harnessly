// An office file open in the pane and edited in place: its book read from the disk, the save that follows an edit by a moment,
// the file changing under the edits (they go on top when they can), and the file read again when the agent writes it.
import { useCallback, useEffect, useRef, useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { FileConflict, fileRevision, saveFileBytes } from '@/next/features/pane/sheet/file-bytes';
import { useFileLook } from '@/next/features/pane/use-file-changes';
import { SAVE, type TSave } from '@/next/features/pane/file-save-state';

const SAVE_PAUSE_MS = 700;
/** A file written again and again under the save: past this the person hears of it. */
const MAX_MERGES = 3;

/** How a kind of file is read, written and compared. Lives at a module's level: the hook reads the file again when it changes. */
export interface IOfficeKind<TBook, TBase> {
  read: (bytes: Uint8Array) => TBook;
  write: (book: TBook) => Uint8Array;
  snapshot: (book: TBook) => TBase;
  /** The edits of `mine` into `theirs`; false when one has nowhere to go, and `theirs` is left unchanged. */
  merge: (base: TBase, mine: TBook, theirs: TBook) => boolean;
}

interface IUseOfficeFile<TBook, TBase> {
  machine: MachineId;
  path: string;
  src: string;
  kind: IOfficeKind<TBook, TBase>;
}

export const useOfficeFile = <TBook, TBase>({ machine, path, src, kind }: IUseOfficeFile<TBook, TBase>) => {
  const current = useRef<TBook | null>(null);
  const base = useRef<TBase | null>(null);
  const revision = useRef<string | null>(null);
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const saving = useRef(false);
  const again = useRef(false);
  const writeOutRef = useRef<(force: boolean) => Promise<void>>(() => Promise.resolve());
  const [book, setBook] = useState<TBook | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const [save, setSave] = useState<TSave>(SAVE.clean);
  const [shownRevision, setShownRevision] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  const readDisk = useCallback(
    async (signal?: AbortSignal) => {
      const res = await fetch(src, { credentials: 'same-origin', signal, cache: 'no-store' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return new Uint8Array(await res.arrayBuffer());
    },
    [src],
  );

  const put = useCallback((next: TBook, rev: string, nextBase: TBase) => {
    current.current = next;
    base.current = nextBase;
    revision.current = rev;
    setShownRevision(rev);
    setBook(next);
  }, []);

  useEffect(() => {
    const abort = new AbortController();
    void (async () => {
      const rev = await fileRevision(machine, path, abort.signal);
      const next = kind.read(await readDisk(abort.signal));
      if (abort.signal.aborted) return;
      put(next, rev, kind.snapshot(next));
      setSave(SAVE.clean);
    })().catch((failure: Error) => {
      if (!abort.signal.aborted) setFailed(failure.message);
    });
    return () => {
      abort.abort();
      // An edit still waiting for its pause is saved on the way out: closing the tab is no reason to lose it.
      if (saveTimer.current !== null) {
        clearTimeout(saveTimer.current);
        saveTimer.current = null;
        void writeOutRef.current(false);
      }
    };
  }, [machine, path, kind, attempt, readDisk, put]);

  // One save at a time: a second one, sent over the revision the first is about to replace, would refuse itself as a conflict.
  const writeOut = useCallback(
    async (force: boolean) => {
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
          const mine = current.current;
          if (mine === null) return;
          const sent = kind.snapshot(mine);
          try {
            revision.current = await saveFileBytes(machine, path, kind.write(mine), over);
            base.current = sent;
            over = revision.current;
          } catch (failure) {
            if (!(failure instanceof FileConflict) || merges >= MAX_MERGES || base.current === null)
              throw failure;
            merges += 1;
            const rev = await fileRevision(machine, path);
            const theirs = kind.read(await readDisk());
            const theirBase = kind.snapshot(theirs);
            if (!kind.merge(base.current, mine, theirs)) throw failure;
            put(theirs, rev, theirBase);
            over = rev;
            again.current = true;
          }
        } while (again.current);
        setShownRevision(revision.current);
        setSave(SAVE.saved);
      } catch (failure) {
        setSave(failure instanceof FileConflict ? SAVE.conflict : SAVE.failed);
      } finally {
        saving.current = false;
      }
    },
    [machine, path, kind, readDisk, put],
  );

  useEffect(() => {
    writeOutRef.current = writeOut;
  }, [writeOut]);

  // The file on the disk moved on (the agent wrote it): with no edit waiting it is read again in place.
  const lookAtDisk = async () => {
    const idle = () => !saving.current && saveTimer.current === null && save !== SAVE.conflict;
    if (!idle()) return;
    const rev = await fileRevision(machine, path).catch(() => null);
    if (rev === null || rev === revision.current || !idle()) return;
    const next = kind.read(await readDisk());
    if (idle()) put(next, rev, kind.snapshot(next));
  };
  useFileLook(() => void lookAtDisk().catch(() => undefined), book !== null);

  /** The book was edited: it goes to the disk a moment after the last edit. */
  const touch = useCallback(() => {
    setSave(SAVE.dirty);
    if (saveTimer.current !== null) clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => {
      saveTimer.current = null;
      void writeOut(false);
    }, SAVE_PAUSE_MS);
  }, [writeOut]);

  return {
    book,
    failed,
    save,
    revision: shownRevision,
    touch,
    overwrite: () => void writeOut(true),
    reload: () => {
      setFailed(null);
      setBook(null);
      setAttempt((n) => n + 1);
    },
  };
};
