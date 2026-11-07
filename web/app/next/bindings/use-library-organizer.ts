// The person's shelf over the machine: folders and where projects lie, the trash and its undo. A change waits
// for the machine before the screen shows it; a refusal is told and thrown so the dialog stays.
import { useCallback, useEffect, useState } from 'react';
import {
  deleteDesign,
  deleteFolder,
  fetchLayout,
  fetchTrash,
  moveToFolder,
  purgeDesign,
  restoreDesign,
  saveFolder,
  type ILibraryLayout,
  type ITrashItem,
} from '@/next/features/designly/api';
import type { ILibraryOrganizer } from '@/next/features/designly/library-organizer';
import { useLinkRestored } from '@/next/stores/connection';
import type { MachineId } from '@/next/protocol/v2';
import { plural, t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';
import { useStableFn } from '@/next/ui/use-stable-fn';

const NO_LAYOUT: ILibraryLayout = { folders: [], placed: {}, systems: {} };

const told = async <T>(title: string, run: () => Promise<T>): Promise<T> => {
  try {
    return await run();
  } catch (failure) {
    toast(title, { tone: 'error', description: (failure as Error).message });
    throw failure;
  }
};

export const useLibraryOrganizer = (
  machine: MachineId,
  /** Reads the library again, after projects came or went. */
  reload: () => Promise<void>,
  /** The projects that went to the trash leave the cards at once. */
  forget: (ids: string[]) => void,
): { layout: ILibraryLayout; organizer: ILibraryOrganizer } => {
  const [layout, setLayout] = useState<ILibraryLayout>(NO_LAYOUT);
  const [trash, setTrash] = useState<ITrashItem[] | null>(null);
  const restored = useLinkRestored(machine);
  const reloadNow = useStableFn(reload);
  const forgetNow = useStableFn(forget);

  const readLayout = useCallback(
    (signal?: AbortSignal) =>
      // A runtime without folders (older) leaves the library as it was: no folders, nothing placed.
      fetchLayout(machine, signal).then(setLayout, () => undefined),
    [machine],
  );
  useEffect(() => {
    const abort = new AbortController();
    void readLayout(abort.signal);
    return () => abort.abort();
  }, [readLayout, restored]);

  const loadTrash = useCallback(
    () => told(t('next.designly.trashFailed'), () => fetchTrash(machine)).then(setTrash),
    [machine],
  );

  const onRestore = useCallback(
    async (id: string) => {
      await told(t('next.designly.restoreFailed'), () => restoreDesign(machine, id));
      setTrash((list) => list?.filter((i) => i.id !== id) ?? null);
      await Promise.all([reloadNow(), readLayout()]);
    },
    [machine, readLayout, reloadNow],
  );

  const onDeleteMany = useCallback(
    async (ids: string[]) => {
      const done: string[] = [];
      let failure: unknown = null;
      for (const id of ids) {
        try {
          await deleteDesign(machine, id);
          done.push(id);
        } catch (e) {
          failure = e;
        }
      }
      forgetNow(done);
      if (failure !== null) {
        toast(t('next.designly.deleteFailed'), { tone: 'error', description: (failure as Error).message });
      }
      if (done.length > 0) {
        toast(plural(done.length, 'next.designly.trashed'), {
          action: {
            label: t('next.designly.undo'),
            onClick: () => {
              void Promise.all(done.map((id) => restoreDesign(machine, id)))
                .then(() => Promise.all([reloadNow(), readLayout()]))
                .catch((e: Error) =>
                  toast(t('next.designly.restoreFailed'), { tone: 'error', description: e.message }),
                );
            },
          },
        });
        setTrash(null);
      }
      if (failure !== null && done.length === 0) throw failure;
    },
    [machine, readLayout, reloadNow, forgetNow],
  );

  const organizer: ILibraryOrganizer = {
    folders: layout.folders,
    onFolderSave: async (input) => {
      await told(t('next.designly.folderFailed'), () => saveFolder(machine, input));
      await readLayout();
    },
    onFolderDelete: async (id, projects) => {
      const res = await told(t('next.designly.folderFailed'), () => deleteFolder(machine, id, projects));
      if (res.trashed.length > 0) forgetNow(res.trashed);
      setTrash(null);
      await Promise.all([readLayout(), reloadNow()]);
    },
    onMove: async (ids, folder) => {
      await told(t('next.designly.moveFailed'), () => moveToFolder(machine, ids, folder));
      await readLayout();
    },
    onDeleteMany,
    trash,
    loadTrash,
    onRestore,
    onPurge: async (id) => {
      await told(t('next.designly.purgeFailed'), () => purgeDesign(machine, id));
      setTrash((list) => list?.filter((i) => i.id !== id) ?? null);
    },
  };
  return { layout, organizer };
};
