// The files of a draft: pick, upload ahead of sending (spec §3), show the progress, keep the uploaded ones in the
// draft. A failed one stays with its reason and a retry until it is removed; nothing uploads for ever (the upload ends
// on silence).
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  newUploadSession,
  UPLOAD_FAILURE,
  keepInHub,
  uploadFile,
  type IUploadSession,
  type UploadOutcome,
} from '@/next/commands/uploads';
import { formatBytes } from '@/next/ui/transfer';
import { useTakeMarked } from '@/next/features/image-edit/to-composer';
import { fitPicture, isHeic, isSqueezable, limitFor, megabytes } from '@/next/commands/photo';
import type { ComposerAttachment } from '@/next/features/composer/types';
import type { AttachmentRef, MachineId } from '@/next/protocol/v2';
import { draftsStore, setDraft, type DraftKey } from '@/next/stores/drafts';
import { limitsOf } from '@/next/stores/machines';
import { connectionStore } from '@/next/stores/connection';
import { appConfig } from '@/platform/config';
import { t } from '@/next/i18n';
import { contentKey } from '@/next/features/composer/distinct-files';
import { toast } from '@/next/ui/toast';

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

/** A row still uploading is named so; anything else is an uploaded file's ref. */
const LOCAL_PREFIX = 'up-';

/**
 * What each file of a draft holds (contentKey: SHA-256 of its bytes), by its row's or its ref's id. Kept
 * outside the hook: two fields of one draft (Sinopia's folded field and its open chat) see the same files.
 */
const contentsOf = new Map<DraftKey, Map<string, string>>();
const contentsFor = (key: DraftKey): Map<string, string> => {
  const have = contentsOf.get(key);
  if (have) return have;
  const made = new Map<string, string>();
  contentsOf.set(key, made);
  return made;
};

/** The contents attached to the draft now: rows uploading and refs still in it. */
const heldBy = (key: DraftKey): Set<string> => {
  const inDraft = new Set((draftsStore.get(key)?.attachments ?? []).map((a) => a.id));
  const held = new Set<string>();
  for (const [id, content] of contentsFor(key)) {
    if (id.startsWith(LOCAL_PREFIX) || inDraft.has(id)) held.add(content);
  }
  return held;
};

/** The draft's files with the same one twice left out (by id, or by what it holds): a message sends each once. */
export const distinctRefs = (files: readonly AttachmentRef[], contents: ReadonlyMap<string, string>): AttachmentRef[] => {
  const ids = new Set<string>();
  const held = new Set<string>();
  return files.filter((file) => {
    const content = contents.get(file.id);
    if (ids.has(file.id) || (content !== undefined && held.has(content))) return false;
    ids.add(file.id);
    if (content !== undefined) held.add(content);
    return true;
  });
};

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
  /** The rows this field made: a failed one leaves with the field, and the message may take that file again. */
  const mine = useRef(new Set<string>());
  useEffect(() => {
    const made = mine.current;
    return () => {
      const contents = contentsFor(key);
      for (const id of made) contents.delete(id);
      made.clear();
    };
  }, [key]);
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
      // a machine that is off gets the file later: it goes into the hub now, and the machine takes it from
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
          const contents = contentsFor(key);
          const content = contents.get(id);
          contents.delete(id);
          mine.current.delete(id);
          if (content !== undefined) contents.set(outcome.ref.id, content);
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
  // A file the message already has (the same bytes) is not attached again: a short notice says so.
  const onFiles = useCallback(
    (files: File[]) => {
      void Promise.all(files.map(contentKey)).then((keys) => {
        const contents = contentsFor(key);
        const held = heldBy(key);
        let twice = false;
        files.forEach((file, i) => {
          const content = keys[i];
          if (held.has(content)) {
            twice = true;
            return;
          }
          held.add(content);
          const id = `${LOCAL_PREFIX}${(seq += 1)}`;
          contents.set(id, content);
          mine.current.add(id);
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
        });
        if (twice) toast(t('next.upload.already'));
      });
    },
    [key, start],
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
      contentsFor(key).delete(id);
      const have = draftsStore.get(key);
      if (have?.attachments.some((a) => a.id === id)) {
        setDraft(key, { ...have, attachments: have.attachments.filter((a) => a.id !== id) });
      }
    },
    [key],
  );

  const reset = useCallback(() => {
    const contents = contentsFor(key);
    for (const row of local) contents.delete(row.id);
    setLocal([]);
  }, [key, local]);
  // A picture marked in the editor (features/image-edit) comes as a file picked with «+»; one marked from an
  // attachment of this field takes its place.
  useTakeMarked(key, {
    onFiles,
    onRemove,
    owns: (id) => draftFiles.some((a) => a.id === id) || local.some((r) => r.id === id),
  });

  const files = useMemo(() => distinctRefs(draftFiles, contentsFor(key)), [draftFiles, key]);
  const attachments: ComposerAttachment[] = [
    ...files.map((a): ComposerAttachment => ({
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
    ready: files,
    uploading: local.some((r) => r.status === 'uploading'),
    onFiles,
    onRemove,
    onRetry,
    reset,
  };
}
