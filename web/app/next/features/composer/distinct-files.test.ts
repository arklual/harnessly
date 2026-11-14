import { describe, expect, it } from 'vitest';
import { contentKey, distinctFiles, pastedFiles } from '@/next/features/composer/distinct-files';

const NOW = 1_800_000_000_000;
const png = (bytes: number[], name = 'image.png', lastModified = NOW) =>
  new File([new Uint8Array(bytes)], name, { type: 'image/png', lastModified });
const jpg = (bytes: number[], name: string) =>
  new File([new Uint8Array(bytes)], name, { type: 'image/jpeg', lastModified: NOW - 86_400_000 });

describe('the files of one paste', () => {
  it('keep one picture that the clipboard holds twice', async () => {
    const files = await distinctFiles([png([1, 2, 3]), png([1, 2, 3], 'Screenshot.png')]);
    expect(files.map((f) => f.name)).toEqual(['image.png']);
  });

  it('keep different files of the same size, in their order', async () => {
    const files = await distinctFiles([png([1, 2, 3], 'a.png'), png([1, 2, 4], 'b.png'), png([9], 'c.png')]);
    expect(files.map((f) => f.name)).toEqual(['a.png', 'b.png', 'c.png']);
  });

  it('leave out the pixels of a picture whose file came too (other bytes, the same picture)', () => {
    const files = pastedFiles([jpg([1, 2, 3, 4], 'photo.jpg'), png([7, 7, 7, 7, 7])], NOW + 50);
    expect(files.map((f) => f.name)).toEqual(['photo.jpg']);
  });

  it('keep the pixels when they are all the clipboard has', () => {
    expect(pastedFiles([png([1, 2, 3])], NOW + 50).map((f) => f.name)).toEqual(['image.png']);
  });

  it('keep an old file that only happens to be called image.png', () => {
    const files = pastedFiles([jpg([1], 'photo.jpg'), png([2], 'image.png', NOW - 86_400_000)], NOW);
    expect(files.map((f) => f.name)).toEqual(['photo.jpg', 'image.png']);
  });
});

describe('what a file holds', () => {
  it('is the SHA-256 of its bytes', async () => {
    const key = await contentKey(new File(['abc'], 'a.txt'));
    expect(key).toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
  });

  it('is the same for the same bytes under other names', async () => {
    expect(await contentKey(png([5, 6], 'a.png'))).toBe(await contentKey(png([5, 6], 'b.png', 1)));
  });
});
