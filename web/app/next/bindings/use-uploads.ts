// The files of a draft: pick, upload ahead of sending (spec §3), show the progress, keep the uploaded ones in the
// draft. A failed one stays with its reason and a retry until it is removed; nothing uploads for ever (the upload ends
// on silence).
import { useCallback, useRef, useState } from 'react';
import { uploadFile } from '@/next/commands';
import { fitPicture, isHeic, isSqueezable, limitFor, megabytes } from '@/next/commands/photo';
import type { ComposerAttachment } from '@/next/features/composer/types';
import type { AttachmentRef, MachineId } from '@/next/protocol/v2';
import { draftsStore, setDraft, type DraftKey } from '@/next/stores/drafts';
import { limitsOf } from '@/next/stores/machines';
import { t } from '@/next/i18n';

interface ILocal {
  id: string;
  name: string;
  kind: 'image' | 'file';
  status: 'uploading' | 'error';
  error: string | null;
  /** The picked file, kept for a retry. */
  file: File;
}

let seq = 0;

export interface IUploads {
  attachments: ComposerAttachment[];
  /** The uploaded files, for the message. */
  ready: AttachmentRef[];
  uploading: boolean;
  /** Files picked with «+», dropped on the field or pasted into it. */
  onFiles: (files: File[]) => void;
  onRemove: (id: string) => void;
  /** Uploads a failed file again. */
  onRetry: (id: string) => void;
  /** Forgets the local rows (after the message went). */
  reset: () => void;
}

export function useUploads(machine: MachineId | null, key: DraftKey, draftFiles: AttachmentRef[]): IUploads {
  const [local, setLocal] = useState<ILocal[]>([]);
  const controllers = useRef(new Map<string, AbortController>());

  const fail = useCallback((id: string, error: string) => {
    controllers.current.delete(id);
    setLocal((rows) => rows.map((r) => (r.id === id ? { ...r, status: 'error', error } : r)));
  }, []);

  const send = useCallback(
    (id: string, file: File, abort: AbortController) => {
      if (!machine) {
        fail(id, t('next.upload.noMachine'));
        return;
      }
      const limits = limitsOf(machine);
      const { bytes } = limitFor(file, limits);
      if (file.size > bytes) {
        fail(id, t('next.upload.tooLarge', { limit: megabytes(bytes) }));
        return;
      }
      void uploadFile(machine, file, () => {}, abort.signal).then((outcome) => {
        controllers.current.delete(id);
        if (outcome.ok) {
          setLocal((rows) => rows.filter((r) => r.id !== id));
          const have = draftsStore.get(key) ?? { text: '', attachments: [] };
          setDraft(key, { ...have, attachments: [...have.attachments, outcome.ref] });
        } else if (outcome.code !== 'aborted') {
          const error = t(
            outcome.code === 'network' || outcome.code === 'http'
              ? `next.failure.${outcome.code}`
              : `next.refuse.${outcome.code}`,
          );
          fail(id, error);
        }
      });
    },
    [machine, key, fail],
  );

  const start = useCallback(
    (id: string, file: File) => {
      if (!machine) {
        fail(id, t('next.upload.noMachine'));
        return;
      }
      const abort = new AbortController();
      controllers.current.set(id, abort);
      const heic = isHeic(file);
      if (!isSqueezable(file)) {
        send(id, file, abort);
        return;
      }
      // A picture past the limit or 4096 px is squeezed here, the iPhone's HEIC becomes a JPEG: the agents read no HEIC.
      const bytes = limitsOf(machine).maxImageBytes;
      void fitPicture(file, bytes).then((fitted) => {
        if (abort.signal.aborted) return;
        if (fitted) send(id, fitted, abort);
        else fail(id, heic ? t('next.upload.heic') : t('next.upload.tooLarge', { limit: megabytes(bytes) }));
      });
    },
    [machine, send, fail],
  );

  // No machine yet (the list still loading): the files are shown with the reason and a retry, never dropped in silence.
  const onFiles = useCallback(
    (files: File[]) => {
      for (const file of files) {
        const id = `up-${(seq += 1)}`;
        const row: ILocal = {
          id,
          name: file.name,
          kind: isHeic(file) || file.type.startsWith('image/') ? 'image' : 'file',
          status: 'uploading',
          error: null,
          file,
        };
        setLocal((rows) => [...rows, row]);
        start(id, file);
      }
    },
    [start],
  );

  const onRetry = useCallback(
    (id: string) => {
      const row = local.find((r) => r.id === id && r.status === 'error');
      if (!row) return;
      setLocal((rows) => rows.map((r) => (r.id === id ? { ...r, status: 'uploading', error: null } : r)));
      start(id, row.file);
    },
    [local, start],
  );

  const onRemove = useCallback(
    (id: string) => {
      controllers.current.get(id)?.abort();
      controllers.current.delete(id);
      setLocal((rows) => rows.filter((r) => r.id !== id));
      const have = draftsStore.get(key);
      if (have?.attachments.some((a) => a.id === id)) {
        setDraft(key, { ...have, attachments: have.attachments.filter((a) => a.id !== id) });
      }
    },
    [key],
  );

  const reset = useCallback(() => setLocal([]), []);

  const attachments: ComposerAttachment[] = [
    ...draftFiles.map((a): ComposerAttachment => ({
      id: a.id,
      name: a.name,
      kind: a.kind,
      status: 'ready',
      error: null,
    })),
    ...local.map((r): ComposerAttachment => ({
      id: r.id,
      name: r.name,
      kind: r.kind,
      status: r.status,
      error: r.error,
    })),
  ];
  return {
    attachments,
    ready: draftFiles,
    uploading: local.some((r) => r.status === 'uploading'),
    onFiles,
    onRemove,
    onRetry,
    reset,
  };
}
