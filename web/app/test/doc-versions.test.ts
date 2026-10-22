import { describe, expect, it } from 'vitest';
import {
  currentVersion,
  findVersion,
  orderedPair,
  parseVersionParam,
  parseVersionRef,
  previousVersion,
  versionId,
} from '@/lib/doc-versions';
import type { IDocVersion } from '@/types/doc-threads';

const version = (n: number, sha: string, current = false): IDocVersion => ({
  n,
  sha,
  createdAt: '2026-10-01T09:00:00.000Z',
  author: 'agent',
  note: null,
  current,
});
const VERSIONS = [version(3, 'c'.repeat(40), true), version(2, 'b'.repeat(40)), version(1, 'a'.repeat(40))];

describe('parseVersionParam', () => {
  it('lowercases a commit hash and passes an id of the artifacts service or a version number as given', () => {
    expect(parseVersionParam('ABCDEF1')).toBe('abcdef1');
    expect(parseVersionParam('20261019T201232Z-9c41ab')).toBe('20261019T201232Z-9c41ab');
    expect(parseVersionParam('3')).toBe('3');
    expect(parseVersionParam('main~3')).toBeNull();
    expect(parseVersionParam('../x')).toBeNull();
    expect(parseVersionParam('-x')).toBeNull();
    expect(parseVersionParam('a'.repeat(65))).toBeNull();
    expect(parseVersionParam(['abcdef1'])).toBeNull();
    expect(parseVersionParam(undefined)).toBeNull();
    expect(parseVersionRef(null)).toBeNull();
  });
});

describe('versions', () => {
  it('finds a version by its full hash or by the prefix of a shared link', () => {
    expect(findVersion(VERSIONS, 'b'.repeat(40))?.n).toBe(2);
    expect(findVersion(VERSIONS, 'a'.repeat(7))?.n).toBe(1);
    expect(findVersion(VERSIONS, null)).toBeNull();
  });

  it('knows the current version and the one before another', () => {
    expect(currentVersion(VERSIONS)?.n).toBe(3);
    expect(previousVersion(VERSIONS, VERSIONS[0])?.n).toBe(2);
    expect(previousVersion(VERSIONS, VERSIONS[2])).toBeNull();
  });

  it('finds a version of the artifacts service by its opaque id, an id prefix or its number', () => {
    const service = [
      { ...version(3, '20261019T201232Z-9c41ab', true), id: '20261019T201232Z-9c41ab' },
      { ...version(2, 'b'.repeat(40)), id: 'b'.repeat(40) },
      { ...version(1, '20261018T101010Z-000001'), id: '20261018T101010Z-000001' },
    ];
    expect(findVersion(service, '20261019T201232Z-9c41ab')?.n).toBe(3);
    expect(findVersion(service, '20261018T1')?.n).toBe(1);
    expect(findVersion(service, '2026101')).toBeNull();
    expect(findVersion(service, '2')?.n).toBe(2);
    expect(findVersion(service, '9')).toBeNull();
    expect(versionId(service[0])).toBe('20261019T201232Z-9c41ab');
    expect(versionId(VERSIONS[0])).toBe('c'.repeat(40));
  });

  it('orders a comparison older to newer whichever way it was picked', () => {
    expect(orderedPair(VERSIONS[0], VERSIONS[2])).toEqual({ from: VERSIONS[2], to: VERSIONS[0] });
    expect(orderedPair(VERSIONS[2], VERSIONS[0])).toEqual({ from: VERSIONS[2], to: VERSIONS[0] });
  });
});
