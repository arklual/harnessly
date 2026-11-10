// Files sent into the folder on screen and a new folder in it (HRN-679): what «Диск» could do, now in «Файлы». The calls
// are «Диск»'s (/api/fs/*): they know the workspace's places by id, and the folder is found among them by its absolute
// path. A folder outside the places takes neither. The queue is the chat's, not the tab's: the pane keeps only the tab on
// screen, and files go on while another tab is looked at.
import { useEffect, useEffectEvent, useState, useSyncExternalStore } from 'react';
import {
  createDiskFolder,
  DiskFailure,
  fetchDiskRoots,
  locateDiskPlace,
  type IDiskRoot,
} from '@/lib/hl-disk-api';
import { browserDiskDeps } from '@/lib/hl-disk-http';
import { collectDropped, picksFromInput, type IDropItem } from '@/lib/hl-disk-drop';
import { DiskUploadQueue, UPLOAD_STATE, isActiveUpload, type IDiskUpload } from '@/lib/hl-disk-uploader';
import { t } from '@/next/i18n';
import type { IFolderTake } from '@/next/features/pane/files-view';

export interface IFolderUploads extends IFolderTake {
  /** The folder takes files: it lies in a place of the workspace. */
  ready: boolean;
  uploads: readonly IDiskUpload[];
  queue: DiskUploadQueue;
}

const queues = new Map<string, DiskUploadQueue>();

// Leaving the page while files go asks first, as «Диск» did.
const warn = (event: BeforeUnloadEvent) => event.preventDefault();
let warning = false;
const guardLeaving = () => {
  const sending = [...queues.values()].some((q) => q.getSnapshot().some((u) => isActiveUpload(u.state)));
  if (sending === warning) return;
  warning = sending;
  if (sending) window.addEventListener('beforeunload', warn);
  else window.removeEventListener('beforeunload', warn);
};

const queueOf = (chat: string): DiskUploadQueue => {
  const have = queues.get(chat);
  if (have) return have;
  const made = new DiskUploadQueue(browserDiskDeps());
  made.subscribe(guardLeaving);
  queues.set(chat, made);
  return made;
};

/** `chat` owns the queue; `folder` is the absolute path of the folder on screen, null while a file is. `onDone` hears
 *  each file that arrived while the tab is on screen. */
export const useFolderUploads = (chat: string, folder: string | null, onDone: () => void): IFolderUploads => {
  const [roots, setRoots] = useState<readonly IDiskRoot[] | null>(null);
  useEffect(() => {
    const abort = new AbortController();
    fetchDiskRoots(abort.signal).then(setRoots, () => {
      if (!abort.signal.aborted) setRoots([]);
    });
    return () => abort.abort();
  }, []);
  const queue = queueOf(chat);
  const uploads = useSyncExternalStore(queue.subscribe, queue.getSnapshot, queue.getSnapshot);
  const doneOf = useEffectEvent(onDone);
  useEffect(() => {
    const doneNow = () => queue.getSnapshot().filter((u) => u.state === UPLOAD_STATE.done).length;
    let arrived = doneNow();
    return queue.subscribe(() => {
      const n = doneNow();
      if (n > arrived) doneOf();
      arrived = n;
    });
  }, [queue]);
  const found = folder !== null && roots !== null ? locateDiskPlace(folder, roots) : null;
  const place = found && { root: found.root.id, path: found.path };
  return {
    ready: place !== null,
    uploads,
    queue,
    fromInput: (files) => {
      const picks = picksFromInput(files);
      if (place && picks.length > 0) queue.enqueue(place, picks);
    },
    fromDrop: async (items) => {
      // The entries are taken before anything is awaited: the browser empties the list once the handler yields.
      const picks = await collectDropped(items as unknown as ArrayLike<IDropItem>);
      if (place && picks.length > 0) queue.enqueue(place, picks);
    },
    makeFolder: async (name) => {
      if (!place) return t('next.files.newFolderNowhere');
      try {
        await createDiskFolder(place, name);
        return null;
      } catch (failure) {
        return failure instanceof DiskFailure ? failure.message : t('next.files.newFolderFailed');
      }
    },
  };
};
