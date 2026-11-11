import { describe, expect, it } from 'vitest';
import {
  SEEN_BUILD_MS,
  SEEN_BUILDS_MAX,
  isOfferableBuild,
  parseSeenBuilds,
  rememberBuild,
  type ISeenBuild,
} from '@/next/lib/app-update';

const A = '/agents/assets/index-aaa.js';
const B = '/agents/assets/index-bbb.js';
const C = '/agents/assets/index-ccc.js';
const NOW = 1_000_000_000;

describe('isOfferableBuild', () => {
  it('offers a build this browser has not had', () => {
    expect(isOfferableBuild(A, B, [{ build: A, at: NOW }], NOW)).toBe(true);
  });

  it('offers nothing when the server serves the running build or none', () => {
    expect(isOfferableBuild(A, A, [], NOW)).toBe(false);
    expect(isOfferableBuild(A, null, [], NOW)).toBe(false);
    expect(isOfferableBuild(null, B, [], NOW)).toBe(false);
  });

  it('does not offer again the build the person has just reloaded for', () => {
    // Half a rollout: the address answers with A and with B in turn. The person ran A, was offered B and reloaded.
    const seen = rememberBuild(rememberBuild([], A, NOW), B, NOW);
    expect(isOfferableBuild(A, B, seen, NOW + 60_000)).toBe(false);
    expect(isOfferableBuild(B, A, seen, NOW + 60_000)).toBe(false);
    expect(isOfferableBuild(A, C, seen, NOW + 60_000)).toBe(true);
  });

  it('offers a build again once it was not seen for long enough', () => {
    const seen: ISeenBuild[] = [{ build: B, at: NOW }];
    expect(isOfferableBuild(A, B, seen, NOW + SEEN_BUILD_MS - 1)).toBe(false);
    expect(isOfferableBuild(A, B, seen, NOW + SEEN_BUILD_MS)).toBe(true);
  });
});

describe('rememberBuild', () => {
  it('keeps one entry per build, newest last', () => {
    const seen = rememberBuild(rememberBuild(rememberBuild([], A, 1), B, 2), A, 3);
    expect(seen).toEqual([
      { build: B, at: 2 },
      { build: A, at: 3 },
    ]);
  });

  it('caps the list', () => {
    let seen: ISeenBuild[] = [];
    for (let i = 0; i < SEEN_BUILDS_MAX + 3; i += 1) seen = rememberBuild(seen, `/b-${i}.js`, i);
    expect(seen).toHaveLength(SEEN_BUILDS_MAX);
    expect(seen[0]?.build).toBe('/b-3.js');
  });
});

describe('parseSeenBuilds', () => {
  it('reads what was written and drops anything else', () => {
    expect(parseSeenBuilds(JSON.stringify([{ build: A, at: 5 }, { build: 7 }, 'x', null]))).toEqual([
      { build: A, at: 5 },
    ]);
    expect(parseSeenBuilds('{"build":"x"}')).toEqual([]);
    expect(parseSeenBuilds('not json')).toEqual([]);
    expect(parseSeenBuilds(null)).toEqual([]);
  });
});
