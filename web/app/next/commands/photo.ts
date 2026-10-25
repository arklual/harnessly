// What a phone hands over before it goes to the machine: a photo in HEIC/HEIF (the iPhone's own format, which the agents
// do not read) becomes a JPEG on the phone, a picture past the machine's limit or wider than 4096 px is squeezed on the
// phone (a screenshot's PNG stays a PNG when it fits), and only what cannot be squeezed is refused before a byte is sent.

import type { Limits } from '@/next/protocol/v2';

/** The pictures the runtime takes as pictures (internal/wire imageMimes); any other type is a file there. */
const PICTURE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'] as const;

export const isPicture = (type: string): boolean => (PICTURE_TYPES as readonly string[]).includes(type);

/** A photo in HEIC/HEIF; the type is empty for it on some systems, so the name says too. */
export const isHeic = (file: { type: string; name?: string }): boolean =>
  /^image\/hei[cf](-sequence)?$/i.test(file.type) || /\.hei[cf]$/i.test(file.name ?? '');

/** The limit in bytes the machine sets for this file, and whether it counts as a picture. */
export const limitFor = (file: { type: string }, limits: Pick<Limits, 'maxImageBytes' | 'maxFileBytes'>) =>
  isPicture(file.type)
    ? { picture: true as const, bytes: limits.maxImageBytes }
    : { picture: false as const, bytes: limits.maxFileBytes };

export const megabytes = (bytes: number): number => Math.floor(bytes / (1024 * 1024));

/** The longest side of what goes out: a phone canvas holds ~16 million pixels, 4096 x 3072 is under that. */
export const LONGEST_SIDE = 4096;
/** JPEG qualities tried at each size, best first; then the picture gets smaller by these steps. */
const QUALITIES = [0.85, 0.7] as const;
const SHRINK = [1, 0.75, 0.5, 0.35] as const;

/** Pictures worth squeezing: a GIF may be an animation and goes as it is. */
const SQUEEZABLE = ['image/png', 'image/jpeg', 'image/webp'] as const;

export const isSqueezable = (file: { type: string; name?: string }): boolean =>
  isHeic(file) || (SQUEEZABLE as readonly string[]).includes(file.type);

/** The name of the converted file: the original's without its extension, `.jpg` instead. */
export const jpegName = (name: string): string => `${name.replace(/\.[^./\\]+$/, '') || 'photo'}.jpg`;

const encode = (canvas: HTMLCanvasElement, type: string, quality?: number): Promise<Blob | null> =>
  new Promise((resolve) => canvas.toBlob(resolve, type, quality));

const drawn = (bitmap: ImageBitmap, scale: number, flat: boolean): HTMLCanvasElement | null => {
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(bitmap.width * scale));
  canvas.height = Math.max(1, Math.round(bitmap.height * scale));
  const context = canvas.getContext('2d');
  if (!context) return null;
  // A JPEG has no transparency: what was clear becomes white, not black.
  if (flat) {
    context.fillStyle = '#fff';
    context.fillRect(0, 0, canvas.width, canvas.height);
  }
  context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  return canvas;
};

/**
 * The picture as it goes out: the file itself when it fits the limit and 4096 px (also when this browser cannot read
 * it and it fits anyway); a PNG as a smaller PNG when that fits; else a JPEG, smaller step by step until it fits.
 * Null when it does not fit and cannot be made to (the browser does not read it, or nothing small enough is left):
 * the caller says so, nothing else is sent in its place.
 */
export async function fitPicture(file: File, limit: number): Promise<File | null> {
  const heic = isHeic(file);
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    return !heic && file.size <= limit ? file : null;
  }
  try {
    const side = Math.max(bitmap.width, bitmap.height);
    if (!heic && file.size <= limit && side <= LONGEST_SIDE) return file;
    const fit = Math.min(1, LONGEST_SIDE / side);
    if (file.type === 'image/png') {
      const canvas = drawn(bitmap, fit, false);
      const png = canvas && (await encode(canvas, 'image/png'));
      if (png && png.size <= limit)
        return new File([png], file.name, { type: 'image/png', lastModified: file.lastModified });
    }
    for (const shrink of SHRINK) {
      const canvas = drawn(bitmap, fit * shrink, true);
      if (!canvas) return null;
      for (const quality of QUALITIES) {
        const blob = await encode(canvas, 'image/jpeg', quality);
        if (blob && blob.size <= limit)
          return new File([blob], jpegName(file.name), {
            type: 'image/jpeg',
            lastModified: file.lastModified,
          });
      }
    }
    return null;
  } finally {
    bitmap.close();
  }
}
