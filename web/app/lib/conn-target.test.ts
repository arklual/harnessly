import { describe, expect, it } from 'vitest';
import { parseConnTarget } from '@/lib/conn-target';

describe('parseConnTarget', () => {
  it('keeps the key as written, with its case, hyphens and colons', () => {
    expect(parseConnTarget('conn:c-1:issue:Ab-Cd-12')).toEqual({
      conn: 'c-1',
      kind: 'issue',
      key: 'Ab-Cd-12',
    });
    expect(parseConnTarget('conn:c-1:page:space:Home:x')).toEqual({
      conn: 'c-1',
      kind: 'page',
      key: 'space:Home:x',
    });
  });
  it('rejects what is not a connection target', () => {
    for (const bad of [
      'PROJ-1',
      'conn:c',
      'conn:c:board:1',
      'conn::issue:1',
      'conn:c:issue:',
      null,
      undefined,
    ])
      expect(parseConnTarget(bad)).toBeNull();
  });
});
