// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  fitPicture,
  isHeic,
  isPicture,
  isSqueezable,
  jpegName,
  limitFor,
  megabytes,
} from '@/next/commands/photo';

const MB = 1024 * 1024;

describe('photo preparation', () => {
  it('knows a HEIC by its type or, where the type is empty, by its name', () => {
    expect(isHeic({ type: 'image/heic', name: 'IMG_1.HEIC' })).toBe(true);
    expect(isHeic({ type: 'image/heif' })).toBe(true);
    expect(isHeic({ type: '', name: 'IMG_1.heic' })).toBe(true);
    expect(isHeic({ type: 'image/jpeg', name: 'IMG_1.jpg' })).toBe(false);
  });

  it('names the converted file', () => {
    expect(jpegName('IMG_0001.HEIC')).toBe('IMG_0001.jpg');
    expect(jpegName('a.b.heif')).toBe('a.b.jpg');
    expect(jpegName('')).toBe('photo.jpg');
  });

  it('takes the machine limit by the kind the runtime gives the file', () => {
    const limits = { maxImageBytes: 10 << 20, maxFileBytes: 50 << 20 };
    expect(isPicture('image/jpeg')).toBe(true);
    expect(isPicture('image/heic')).toBe(false);
    expect(limitFor({ type: 'image/png' }, limits).bytes).toBe(10 << 20);
    expect(limitFor({ type: 'application/pdf' }, limits).bytes).toBe(50 << 20);
    expect(megabytes(10 << 20)).toBe(10);
  });

  it('squeezes PNG, JPEG, WebP and HEIC, leaves a GIF and other files as they are', () => {
    for (const type of ['image/png', 'image/jpeg', 'image/webp', 'image/heic'])
      expect(isSqueezable({ type })).toBe(true);
    expect(isSqueezable({ type: 'image/gif' })).toBe(false);
    expect(isSqueezable({ type: 'application/pdf' })).toBe(false);
    expect(isSqueezable({ type: '', name: 'a.heic' })).toBe(true);
  });
});

// A canvas that is only a size: what it encodes weighs width x height x (4 for a PNG, the quality for a JPEG) / 8 bytes.
const canvases: { width: number; height: number; filled: boolean }[] = [];
const sizeOf = (type: string, quality: number | undefined, w: number, h: number) =>
  Math.round((w * h * (type === 'image/png' ? 4 : (quality ?? 1))) / 8);

beforeEach(() => {
  canvases.length = 0;
  const real = document.createElement.bind(document);
  vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    if (tag !== 'canvas') return real(tag);
    const state = { width: 0, height: 0, filled: false };
    canvases.push(state);
    return {
      get width() {
        return state.width;
      },
      set width(v: number) {
        state.width = v;
      },
      get height() {
        return state.height;
      },
      set height(v: number) {
        state.height = v;
      },
      getContext: () => ({
        fillStyle: '',
        fillRect: () => {
          state.filled = true;
        },
        drawImage: () => undefined,
      }),
      toBlob: (done: (b: Blob) => void, type: string, quality?: number) =>
        done(new Blob([new Uint8Array(sizeOf(type, quality, state.width, state.height))], { type })),
    } as unknown as HTMLCanvasElement;
  });
});
afterEach(() => vi.restoreAllMocks());

const decodes = (width: number, height: number) =>
  vi.stubGlobal('createImageBitmap', () => Promise.resolve({ width, height, close: () => undefined }));
const picture = (name: string, type: string, bytes: number) =>
  new File([new Uint8Array(bytes)], name, { type });

describe('fitPicture', () => {
  it('sends a picture that fits as it is', async () => {
    decodes(4000, 3000);
    const file = picture('a.jpg', 'image/jpeg', 8 * MB);
    expect(await fitPicture(file, 10 * MB)).toBe(file);
    expect(canvases).toHaveLength(0);
  });

  it('squeezes a JPEG past the limit into a JPEG under it, 4096 px at most', async () => {
    decodes(8000, 6000);
    const out = await fitPicture(picture('big.jpg', 'image/jpeg', 11 * MB), 10 * MB);
    expect(out?.type).toBe('image/jpeg');
    expect(out?.name).toBe('big.jpg');
    expect(out && out.size <= 10 * MB).toBe(true);
    expect(Math.max(...canvases.map((c) => Math.max(c.width, c.height)))).toBeLessThanOrEqual(4096);
    expect(canvases.every((c) => c.filled)).toBe(true);
  });

  it('shrinks a small file that is wider than 4096 px', async () => {
    decodes(6000, 4000);
    const out = await fitPicture(picture('wide.jpg', 'image/jpeg', 2 * MB), 10 * MB);
    expect(out).not.toBeNull();
    expect(canvases[0]?.width).toBe(4096);
  });

  it('steps down in quality and size until it fits', async () => {
    decodes(4096, 4096);
    // 4096 x 4096 / 8 x 0.85 = 1.7 MB: a 1 MB limit takes smaller steps.
    const out = await fitPicture(picture('p.jpg', 'image/jpeg', 3 * MB), 1 * MB);
    expect(out && out.size <= 1 * MB).toBe(true);
    expect(canvases.length).toBeGreaterThan(1);
  });

  it('keeps a PNG a PNG while it fits, a clear background stays clear', async () => {
    decodes(2000, 1000);
    const out = await fitPicture(picture('shot.png', 'image/png', 11 * MB), 10 * MB);
    expect(out?.type).toBe('image/png');
    expect(out?.name).toBe('shot.png');
    expect(canvases[0]?.filled).toBe(false);
  });

  it('turns a PNG that does not fit as a PNG into a JPEG', async () => {
    decodes(4000, 3000);
    const out = await fitPicture(picture('shot.png', 'image/png', 20 * MB), 5 * MB);
    expect(out?.type).toBe('image/jpeg');
    expect(out?.name).toBe('shot.jpg');
  });

  it('turns a HEIC into a JPEG even when it is small', async () => {
    decodes(4032, 3024);
    const out = await fitPicture(picture('IMG_1.HEIC', 'image/heic', 2 * MB), 10 * MB);
    expect(out?.type).toBe('image/jpeg');
    expect(out?.name).toBe('IMG_1.jpg');
  });

  it('says it cannot when the browser does not read the picture that is over the limit, or a HEIC', async () => {
    vi.stubGlobal('createImageBitmap', () => Promise.reject(new Error('no decoder')));
    expect(await fitPicture(picture('big.jpg', 'image/jpeg', 11 * MB), 10 * MB)).toBeNull();
    expect(await fitPicture(picture('IMG_1.heic', 'image/heic', 1 * MB), 10 * MB)).toBeNull();
    const fits = picture('ok.jpg', 'image/jpeg', 1 * MB);
    expect(await fitPicture(fits, 10 * MB)).toBe(fits);
  });
});
