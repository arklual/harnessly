// The files of a draft: pick, upload ahead of sending (spec §3), show the progress, keep the uploaded ones in the
// draft. A failed one stays with its reason and a retry until it is removed; nothing uploads for ever (the upload ends
// on silence).
import { useCallback, useRef, useState } from 'react';
import {
  newUploadSession,
  UPLOAD_FAILURE,
  keepInHub,
  uploadFile,
  type IUploadSession,
  type UploadOutcome,
} from '@/next/commands/uploads';
import { formatBytes } from '@/next/ui/transfer';
import { fitPicture, isHeic, isSqueezable, limitFor, megabytes } from '@/next/commands/photo';
import type { ComposerAttachment } from '@/next/features/composer/types';
import type { AttachmentRef, MachineId } from '@/next/protocol/v2';
import { draftsStore, setDraft, type DraftKey } from '@/next/stores/drafts';
import { limitsOf } from '@/next/stores/machines';
import { connectionStore } from '@/next/stores/connection';
import { appConfig } from '@/platform/config';
import { t } from '@/next/i18n';

interface ILocal {
  id: string;
  name: string;
  kind: 'image' | 'file';
  status: 'uploading' | 'error';
  error: string | null;
  /** The picked file, kept for a retry. */
  file: File;
  sent: number;
  speed: number;
}

/** The speed is smoothed over the last seconds so the time left does not jump. */
const SPEED_SMOOTHING = 0.7;
const SHOW_EVERY_MS = 150;

/** Why a file did not go, in words the person can act on. */
const failureText = (outcome: Extract<UploadOutcome, { ok: false }>): string => {
  const { code } = outcome;
  if (code === UPLOAD_FAILURE.noSpace)
    return outcome.need !== undefined && outcome.free !== undefined
      ? t('next.upload.noSpaceNumbers', { need: formatBytes(outcome.need), free: formatBytes(outcome.free) })
      : t('next.upload.noSpace');
  if (code === UPLOAD_FAILURE.network) return t('next.upload.network');
  if (code === UPLOAD_FAILURE.refused || code === UPLOAD_FAILURE.tooLarge)
    return outcome.message ?? t('next.refuse.attachment-too-large');
  if (code === UPLOAD_FAILURE.http || code === 'timeout') return t(`next.failure.${code}`);
  return t(`next.refuse.${code}`);
};

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
  const sessions = useRef(new Map<string, IUploadSession>());
  const rowSession = useCallback((id: string) => {
    const have = sessions.current.get(id);
    if (have) return have;
    const made = newUploadSession();
    sessions.current.set(id, made);
    return made;
  }, []);

  const fail = useCallback((id: string, error: string) => {
    controllers.current.delete(id);
    setLocal((rows) => rows.map((r) => (r.id === id ? { ...r, status: 'error', error } : r)));
  }, []);

  const progressOf = useCallback(
    (id: string, sent: number, mark: { at: number; sent: number; speed: number }) => {
      const now = performance.now();
      if (now - mark.at < SHOW_EVERY_MS) return;
      const instant = ((sent - mark.sent) / (now - mark.at)) * 1000;
      const speed = mark.speed > 0 ? mark.speed * SPEED_SMOOTHING + instant * (1 - SPEED_SMOOTHING) : instant;
      mark.at = now;
      mark.sent = sent;
      mark.speed = Math.max(0, speed);
      setLocal((rows) => rows.map((r) => (r.id === id ? { ...r, sent, speed: mark.speed } : r)));
    },
    [],
  );

  const send = useCallback(
    (id: string, file: File, abort: AbortController) => {
      if (!machine) {
        fail(id, t('next.upload.noMachine'));
        return;
      }
      const limits = limitsOf(machine);
      const { bytes } = limitFor(file, limits);
      // A limit of 0 is none: a file is held back only by the free space of the machine's disk.
      if (bytes > 0 && file.size > bytes) {
        fail(id, t('next.upload.tooLarge', { limit: megabytes(bytes) }));
        return;
      }
      const mark = { at: performance.now(), sent: 0, speed: 0 };
      const session = rowSession(id);
      // HRN-879: a machine that is off gets the file later: it goes into the hub now, and the machine takes it from
      // there when the message leaves the outbox.
      const offline = appConfig().centralUploads && connectionStore.get(machine)?.k !== 'live';
      const road = offline
        ? keepInHub(machine, file, (sent) => progressOf(id, sent, mark))
        : uploadFile(machine, file, (sent) => progressOf(id, sent, mark), abort.signal, session);
      void road.then((outcome) => {
        controllers.current.delete(id);
        if (outcome.ok) {
          setLocal((rows) => rows.filter((r) => r.id !== id));
          sessions.current.delete(id);
          const have = draftsStore.get(key) ?? { text: '', attachments: [] };
          setDraft(key, { ...have, attachments: [...have.attachments, outcome.ref] });
        } else if (outcome.code !== UPLOAD_FAILURE.aborted) {
          fail(id, failureText(outcome));
        }
      });
    },
    [machine, key, fail, progressOf, rowSession],
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
          sent: 0,
          speed: 0,
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
      setLocal((rows) =>
        rows.map((r) => (r.id === id ? { ...r, status: 'uploading', error: null, speed: 0 } : r)),
      );
      start(id, row.file);
    },
    [local, start],
  );

  const onRemove = useCallback(
    (id: string) => {
      controllers.current.get(id)?.abort();
      controllers.current.delete(id);
      setLocal((rows) => rows.filter((r) => r.id !== id));
      sessions.current.delete(id);
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
      progress: r.status === 'uploading' ? { sent: r.sent, size: r.file.size, speed: r.speed } : undefined,
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
