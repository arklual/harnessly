import { describe, expect, it } from 'vitest';
import { LOADER_LINES, loaderLine } from '@/next/features/designly/site-loader';
import { LOAD_STAGE } from '@/next/features/designly/site-load';

describe('the wait’s line', () => {
  it('goes round the usual lines', () => {
    expect(loaderLine(LOAD_STAGE.opening, 0)).toBe(LOADER_LINES[0]);
    expect(loaderLine(LOAD_STAGE.resources, 1)).toBe(LOADER_LINES[1]);
    expect(loaderLine(LOAD_STAGE.opening, LOADER_LINES.length)).toBe(LOADER_LINES[0]);
  });

  it('says a long wait is long', () => {
    expect(loaderLine(LOAD_STAGE.slow, 2)).toBe('next.site.loadSlow');
  });
});
