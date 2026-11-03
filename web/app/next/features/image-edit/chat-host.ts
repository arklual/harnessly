// The IImageEditHost of a chat: its pictures are files on the machine, changed through `<platform>/images` (a job per
// change, kept by the machine across restarts). A version is named by its file's absolute path.
import { useMemo } from 'react';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { listJobs } from '@/next/features/chat/image-jobs';
import { chainVersions, CUT_MODE } from '@/next/features/image-edit/chain';
import type {
  IEditProgress,
  IImageEdit,
  IImageEditHost,
  IImageVersion,
} from '@/next/features/image-edit/host';
import type { IImageEditing } from '@/next/features/image-edit/image-editor';
import { previewUrl } from '@/next/features/pane/delivered';
import { t } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';

export interface IChatImages {
  machine: MachineId;
  conv: ConvId;
  /** Where a picture's bytes are; absent: the machine's file viewer. */
  srcOf?: (path: string) => string;
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;

/** A picture file of the chat as a version. */
export const versionOfPath = (chat: IChatImages, path: string): IImageVersion => ({
  id: path,
  src: chat.srcOf ? chat.srcOf(path) : previewUrl(chat.machine, path),
});

const imagesUrl = (chat: IChatImages, query = '') =>
  runtimePath(platformPath(`/images${query}`), chat.machine);

const errorOf = async (res: Response): Promise<string> => {
  try {
    const doc: unknown = await res.json();
    if (isRecord(doc) && typeof doc.error === 'string' && doc.error !== '') return doc.error;
  } catch {
    // Not JSON: the status below says it.
  }
  return `HTTP ${res.status}`;
};

const decodeSnapshot = (
  doc: unknown,
): { id: string; state: string; paths: string[]; mode: string; error: string } => {
  if (!isRecord(doc) || typeof doc.id !== 'string' || typeof doc.state !== 'string')
    throw new Error('bad answer');
  const images = Array.isArray(doc.images) ? doc.images : [];
  const paths = images
    .map((image) => (isRecord(image) && typeof image.path === 'string' ? image.path : ''))
    .filter((path) => path !== '');
  return {
    id: doc.id,
    state: doc.state,
    paths,
    mode: typeof doc.mode === 'string' ? doc.mode : '',
    error: typeof doc.error === 'string' ? doc.error : '',
  };
};

export const chatImageHost = (chat: IChatImages): IImageEditHost => ({
  opensLatest: true,

  start: async (edit: IImageEdit) => {
    const body = {
      source: edit.from.id,
      mode: edit.mode,
      prompt: edit.prompt,
      format: edit.format,
      overlay: edit.overlay,
      conv: chat.conv,
    };
    const res = await fetch(imagesUrl(chat), {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    // 429 (too many pictures at once) and the others: the server's own reason, which the person may read.
    if (!res.ok) throw new Error(await errorOf(res));
    return decodeSnapshot(await res.json()).id;
  },

  read: async (run, signal): Promise<IEditProgress> => {
    const res = await fetch(imagesUrl(chat, `?id=${encodeURIComponent(run)}`), {
      credentials: 'same-origin',
      signal,
    });
    if (!res.ok) throw new Error(await errorOf(res));
    const job = decodeSnapshot(await res.json());
    if (job.state === 'running') return { state: 'running', versions: [] };
    if (job.state === 'done') {
      const path = job.paths[job.paths.length - 1];
      if (path === undefined) return { state: 'failed', versions: [], error: t('next.imageEdit.noPicture') };
      const version = versionOfPath(chat, path);
      return { state: 'done', versions: [job.mode === CUT_MODE ? { ...version, clear: true } : version] };
    }
    return { state: 'failed', versions: [], error: job.error === '' ? undefined : job.error };
  },

  versions: async (original, signal) => {
    const jobs = await listJobs(chat.machine, chat.conv, signal);
    return chainVersions(original, jobs, (path) => versionOfPath(chat, path).src);
  },
});

/**
 * What the viewer of a chat's picture needs to change it: the chat's host (or the one the page brought, `custom`) and
 * the picture as its first version. Null where the picture cannot be changed (no chat, no picture).
 */
export const useChatEditing = (
  chat: (IChatImages & { edit?: IImageEditHost }) | null,
  original: IImageVersion | null,
): IImageEditing | null => {
  const machine = chat?.machine;
  const conv = chat?.conv;
  const srcOf = chat?.srcOf;
  const custom = chat?.edit;
  const host = useMemo<IImageEditHost | null>(() => {
    if (custom) return custom;
    if (machine === undefined || conv === undefined) return null;
    return chatImageHost({ machine, conv, srcOf });
  }, [custom, machine, conv, srcOf]);
  const id = original?.id;
  const src = original?.src;
  return useMemo(
    () => (host && id !== undefined && src !== undefined ? { original: { id, src }, host } : null),
    [host, id, src],
  );
};
