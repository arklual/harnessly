// One picture, one attachment. A clipboard can hold the same picture twice — the file it came
// from (a messenger, a file manager) and its pixels, which the browser hands over as a fresh `image.png` with other
// bytes — and a message may be handed the same file again (a second paste, a drop on two fields). Files are told
// apart by what they hold: the SHA-256 of their bytes.

/** Past this a file is not read whole to be told apart: its name, size and date stand for its bytes. */
const HASH_LIMIT = 64 * 1024 * 1024;
/** The pixels of a clipboard come as a file made at the paste: dated a moment ago. */
const BITMAP_FRESH_MS = 10_000;

const hex = (buffer: ArrayBuffer): string =>
  [...new Uint8Array(buffer)].map((byte) => byte.toString(16).padStart(2, '0')).join('');

/** What a file holds, as a key: two files with the same bytes have the same one. */
export const contentKey = async (file: File): Promise<string> => {
  // A page over plain http (a machine on the local network) has no crypto.subtle.
  if (file.size > HASH_LIMIT || !globalThis.crypto?.subtle) return `${file.name}:${file.size}:${file.lastModified}`;
  return hex(await crypto.subtle.digest('SHA-256', await file.arrayBuffer()));
};

/** The picture the browser made of the clipboard's pixels (Chrome and Safari name it image.png). */
const isClipboardBitmap = (file: File, now: number): boolean =>
  file.name === 'image.png' && file.type === 'image/png' && Math.abs(now - file.lastModified) < BITMAP_FRESH_MS;

/**
 * The files of one paste: where the clipboard holds a picture's file, the browser's copy of its pixels is the same
 * picture and is left out.
 */
export const pastedFiles = (files: readonly File[], now = Date.now()): File[] => {
  const named = files.some((file) => file.type.startsWith('image/') && !isClipboardBitmap(file, now));
  if (!named) return [...files];
  return files.filter((file) => !isClipboardBitmap(file, now));
};

/** The files with duplicates by content left out, in the order they came. */
export const distinctFiles = async (files: readonly File[]): Promise<File[]> => {
  if (files.length < 2) return [...files];
  const keys = await Promise.all(files.map(contentKey));
  return files.filter((_, i) => keys.indexOf(keys[i]) === i);
};
