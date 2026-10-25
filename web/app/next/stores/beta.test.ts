import { describe, expect, it } from 'vitest';
import { DEFAULT_BETA, parseBeta } from '@/next/stores/beta';

describe('parseBeta', () => {
  it('is off without a record or with a broken one', () => {
    expect(parseBeta(null)).toEqual(DEFAULT_BETA);
    expect(parseBeta('{oops')).toEqual(DEFAULT_BETA);
    expect(parseBeta('7')).toEqual(DEFAULT_BETA);
  });

  it('takes the flags it knows and only booleans', () => {
    expect(parseBeta('{"retitle":true,"multiModel":"yes","other":true}')).toEqual({
      retitle: true,
      multiModel: false,
    });
  });
});
