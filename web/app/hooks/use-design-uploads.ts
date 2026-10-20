import { useCallback, useEffect, useRef, useState } from 'react';
import { currentMachineId } from '@/lib/app-path';
import { uploadDesignProjectWithProgress } from '@/lib/hl-design-library-client';
import { boardsLabel } from '@/lib/hl-design-library/home-model';
import { t } from '@/lib/i18n';

export const UPLOAD_STATUS = {
  waiting: 'waiting',
  sending: 'sending',
  unpacking: 'unpacking',
  done: 'done',
  error: 'error',
} as const;
export type TUploadStatus = (typeof UPLOAD_STATUS)[keyof typeof UPLOAD_STATUS];

export interface IUploadItem {
  id: number;
  name: string;
  status: TUploadStatus;
  /** Share of the archive the runtime has received, 0..1. */
  progress: number;
  /** The result: what was made, or why it was refused. */
  message: string | null;
}

const isZip = (file: File): boolean => /\.zip$/i.test(file.name) || file.type === 'application/zip';

/**
 * Archives dropped on the library or chosen with "Upload zip", sent one after another. Each file shows
 * its own progress and result; an item changes only when the runtime has answered. `onUploaded` runs after every
 * project that joined the library.
 */
const useDesignUploads = (onUploaded: () => Promise<void> | void) => {
  const [items, setItems] = useState<IUploadItem[]>([]);
  const nextId = useRef(0);
  const queue = useRef<Promise<void>>(Promise.resolve());
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const patch = useCallback((id: number, change: Partial<IUploadItem>) => {
    if (alive.current)
      setItems((current) => current.map((item) => (item.id === id ? { ...item, ...change } : item)));
  }, []);

  const sendOne = useCallback(
    async (id: number, file: File) => {
      patch(id, { status: UPLOAD_STATUS.sending });
      try {
        const result = await uploadDesignProjectWithProgress(currentMachineId(), file, (progress) =>
          patch(id, { progress, status: progress >= 1 ? UPLOAD_STATUS.unpacking : UPLOAD_STATUS.sending }),
        );
        patch(id, {
          status: UPLOAD_STATUS.done,
          progress: 1,
          message: `«${result.title}», ${boardsLabel(result.boards)}`,
        });
        await onUploaded();
      } catch (error) {
        patch(id, {
          status: UPLOAD_STATUS.error,
          message: error instanceof Error ? error.message : t('hooks.useDesignUploads.uploadFailed'),
        });
      }
    },
    [onUploaded, patch],
  );

  const add = useCallback(
    (files: readonly File[]) => {
      const added = files.map((file) => ({
        file,
        item: {
          id: (nextId.current += 1),
          name: file.name,
          status: UPLOAD_STATUS.waiting,
          progress: 0,
          message: null,
        } as IUploadItem,
      }));
      const wrong = added.filter(({ file }) => !isZip(file));
      setItems((current) => [
        ...current,
        ...added.map(({ item }) =>
          wrong.some((other) => other.item.id === item.id)
            ? {
                ...item,
                status: UPLOAD_STATUS.error,
                message: t('hooks.useDesignUploads.needZip'),
              }
            : item,
        ),
      ]);
      const right = added.filter(({ file }) => isZip(file));
      queue.current = queue.current.then(async () => {
        for (const { file, item } of right) await sendOne(item.id, file);
      });
    },
    [sendOne],
  );

  const dismiss = useCallback(
    () =>
      setItems((current) =>
        current.filter(
          (item) =>
            item.status === UPLOAD_STATUS.waiting ||
            item.status === UPLOAD_STATUS.sending ||
            item.status === UPLOAD_STATUS.unpacking,
        ),
      ),
    [],
  );

  const busy = items.some(
    (item) =>
      item.status === UPLOAD_STATUS.waiting ||
      item.status === UPLOAD_STATUS.sending ||
      item.status === UPLOAD_STATUS.unpacking,
  );
  return { items, busy, add, dismiss };
};

export default useDesignUploads;
