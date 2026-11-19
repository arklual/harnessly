import { describe, expect, it } from 'vitest';

import { parseCaps } from './builder';

describe('parseCaps', () => {
  it('reads labels separated by commas or spaces, lower case, no duplicates', () => {
    expect(parseCaps('Blender, gpu  ffmpeg,blender,')).toEqual(['blender', 'gpu', 'ffmpeg']);
    expect(parseCaps('')).toEqual([]);
  });
});
