import { afterEach, describe, expect, it, vi } from 'vitest';
import type { DesignArtifact, MachineId } from '@/next/protocol/v2';
import { designsOfHits, mergeDesigns, resetIndexSearch, searchIndex } from './index-search';

const M = 'm-1' as MachineId;

afterEach(() => {
  vi.unstubAllGlobals();
  resetIndexSearch();
});

describe('searchIndex', () => {
  it('returns the hits and keeps designs only', async () => {
    const hits = [
      { kind: 'design', id: 'a', title: 'Alpha', updatedAt: 5 },
      { kind: 'app', id: 'b', title: 'Beta', updatedAt: 1 },
    ];
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ hits }) }),
    );
    const got = await searchIndex(M, 'al');
    expect(got).toHaveLength(2);
    expect(designsOfHits(got ?? []).map((d) => d.id)).toEqual(['a']);
  });

  it('on 404 gives null and is not asked again', async () => {
    const f = vi.fn().mockResolvedValue({ ok: false, status: 404 });
    vi.stubGlobal('fetch', f);
    expect(await searchIndex(M, 'x')).toBeNull();
    expect(await searchIndex(M, 'x')).toBeNull();
    expect(f).toHaveBeenCalledTimes(1);
  });
});

describe('mergeDesigns', () => {
  it('adds only the designs the library lacks', () => {
    const d = (id: string) => ({ id, title: id }) as DesignArtifact;
    expect(mergeDesigns([d('a')], [d('a'), d('b')])?.map((x) => x.id)).toEqual(['a', 'b']);
    expect(mergeDesigns(null, [])).toBeNull();
  });
});
