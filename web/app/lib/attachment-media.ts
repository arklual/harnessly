export type TAttachmentKind = 'image' | 'video' | 'audio' | 'pdf' | 'file';

export interface IUploadedFile {
  path: string;
  name: string;
  kind: Exclude<TAttachmentKind, 'file'>;
  url: string;
}

const EXT_KIND: Record<string, TAttachmentKind> = {
  png: 'image',
  jpg: 'image',
  jpeg: 'image',
  gif: 'image',
  webp: 'image',
  mp4: 'video',
  webm: 'video',
  mov: 'video',
  m4v: 'video',
  ogv: 'video',
  mp3: 'audio',
  wav: 'audio',
  ogg: 'audio',
  oga: 'audio',
  m4a: 'audio',
  aac: 'audio',
  flac: 'audio',
  opus: 'audio',
  weba: 'audio',
  pdf: 'pdf',
};

// Uploads live in the runtime's data directory (~/.harnessly by default, AGENT_DATA_DIR elsewhere): `<data>/uploads/`.
/** `/.harnessly/uploads/` and the like: a hidden data directory's uploads folder. */
const UPLOADS_MARKER = /\/\.[a-z][a-z0-9-]*\/uploads\//;
const UPLOAD_PATH_IN_TEXT = /(\/(?:[^\s\\]|\\.)*?\/\.[a-z][a-z0-9-]*\/uploads\/(?:[^\s\\]|\\.)+)/g;
const TRAILING_PUNCTUATION = /[.,;:!?)\]]+$/;

const extensionOf = (name: string): string =>
  name.includes('.') ? (name.split('.').pop()?.toLowerCase() ?? '') : '';

export const attachmentKindOf = (mime: string, name = ''): TAttachmentKind => {
  if (mime.startsWith('image/')) return mime === 'image/svg+xml' ? 'file' : 'image';
  if (mime.startsWith('video/')) return 'video';
  if (mime.startsWith('audio/')) return 'audio';
  if (mime === 'application/pdf') return 'pdf';
  return EXT_KIND[extensionOf(name)] ?? 'file';
};

export const uploadUrlOfPath = (filePath: string): string | null => {
  const marker = UPLOADS_MARKER.exec(filePath);
  if (!marker || filePath.includes('\0')) return null;
  const segments = filePath.slice(marker.index + marker[0].length).split('/');
  if (segments.some((part) => !part || part === '.' || part === '..')) return null;
  return `/api/uploads/${segments.map(encodeURIComponent).join('/')}`;
};

/** Uploaded files a prompt refers to by path: the ones the viewer can show, once each, in order of mention. */
export const uploadedFilesInText = (text: string): IUploadedFile[] => {
  const found = new Map<string, IUploadedFile>();
  for (const match of text.matchAll(UPLOAD_PATH_IN_TEXT)) {
    const filePath = match[1].replace(TRAILING_PUNCTUATION, '').replace(/\\(.)/g, '$1');
    if (found.has(filePath)) continue;
    const name = filePath.split('/').at(-1) ?? '';
    const kind = attachmentKindOf('', name);
    const url = uploadUrlOfPath(filePath);
    if (kind === 'file' || !url) continue;
    found.set(filePath, { path: filePath, name, kind, url });
  }
  return [...found.values()];
};

/** Claude Code puts `[Image #3]` into the text of a message with pasted pictures; the pictures themselves are drawn. */
export const stripImagePlaceholders = (text: string): string =>
  text.replace(/[ \t]*\[Image #\d+\]/g, '').trim();
