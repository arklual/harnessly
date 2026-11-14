import { describe, expect, it } from 'vitest';
import { distinctFiles } from '@/next/features/composer/distinct-files';

const png = (bytes: number[], name = 'image.png') =>
  new File([new Uint8Array(bytes)], name, { type: 'image/png' });

describe('the files of one paste', () => {
  it('keep one picture that the clipboard holds twice', async () => {
    const files = await distinctFiles([png([1, 2, 3]), png([1, 2, 3], 'Screenshot.png')]);
    expect(files.map((f) => f.name)).toEqual(['image.png']);
  });

  it('keep different files of the same size, in their order', async () => {
    const files = await distinctFiles([png([1, 2, 3], 'a.png'), png([1, 2, 4], 'b.png'), png([9], 'c.png')]);
    expect(files.map((f) => f.name)).toEqual(['a.png', 'b.png', 'c.png']);
  });
});
