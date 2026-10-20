import { describe, expect, it, vi } from 'vitest';

// The workspace of the compose file: boards and artifacts on other ports of localhost, over http.
vi.mock('@/platform/config', async (importOriginal) => {
  const real = await importOriginal<typeof import('@/platform/config')>();
  return {
    ...real,
    appConfig: () => ({
      ...real.appConfig(),
      boardsUrl: 'http://localhost:8081',
      artifactsUrl: 'http://localhost:8082',
      trackerHosts: ['tracker.example.com'],
      gitlabHosts: ['gitlab.example.com'],
    }),
  };
});

const { parseLinkTarget, linkTargetKey } = await import('@/lib/link-meta');

describe('link metadata is looked up by origin', () => {
  it('accepts documents and boards on the configured ports over http', () => {
    expect(parseLinkTarget('http://localhost:8082/a/plan-1a2b3c')).toEqual({
      kind: 'artifact',
      url: 'http://localhost:8082/a/plan-1a2b3c',
    });
    expect(parseLinkTarget('http://localhost:8081/b/flow-4d5e6f?k=abc123#frag')).toEqual({
      kind: 'artifact',
      url: 'http://localhost:8081/b/flow-4d5e6f?k=abc123',
    });
  });

  it('keeps the cache key apart per origin', () => {
    const doc = parseLinkTarget('http://localhost:8082/a/plan-1a2b3c')!;
    const board = parseLinkTarget('http://localhost:8081/a/plan-1a2b3c')!;
    expect(linkTargetKey(doc)).not.toBe(linkTargetKey(board));
  });

  it('refuses any other scheme, port or host of the same machine', () => {
    for (const url of [
      'https://localhost:8082/a/plan-1a2b3c',
      'http://localhost:9999/a/plan-1a2b3c',
      'http://localhost/a/plan-1a2b3c',
      'http://127.0.0.1:8082/a/plan-1a2b3c',
      'http://user:pass@localhost:8082/a/plan-1a2b3c',
      'http://localhost:8082/a/api',
      'http://localhost:8082/a/plan-1a2b3c/extra',
      'http://localhost:8082/a/plan-1a2b3c?k=bad key',
    ]) {
      expect(parseLinkTarget(url), url).toBeNull();
    }
  });

  it('still takes only plain https for the tracker and GitLab', () => {
    expect(parseLinkTarget('https://tracker.example.com/issue/ABC-12')).toEqual({
      kind: 'issue',
      key: 'ABC-12',
    });
    expect(parseLinkTarget('http://tracker.example.com/issue/ABC-12')).toBeNull();
    expect(parseLinkTarget('https://tracker.example.com:8443/issue/ABC-12')).toBeNull();
    expect(parseLinkTarget('https://gitlab.example.com/a/b/-/merge_requests/3')).toMatchObject({
      kind: 'mr',
      iid: 3,
    });
    expect(parseLinkTarget('http://gitlab.example.com/a/b/-/merge_requests/3')).toBeNull();
  });
});
