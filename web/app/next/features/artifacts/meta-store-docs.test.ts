// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ARTIFACT_KIND, type IArtifactItem } from '@/next/features/artifacts/model';
import { requestMetas, resetMetaStore, setMetaFetch } from '@/next/features/artifacts/meta-store';

const item = (url: string, kind: IArtifactItem['kind'] = ARTIFACT_KIND.doc): IArtifactItem => ({
  url,
  kind,
  key: null,
  label: null,
  fallback: 'Document',
  where: url,
  github: false,
  readable: true,
  at: 1,
});

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
  resetMetaStore();
});
afterEach(() => {
  setMetaFetch(null);
  vi.useRealTimers();
});

describe('a document of the catalogue', () => {
  it('takes its title from the gateway catalogue, not from the runtime that cannot read it', async () => {
    const asked: string[] = [];
    setMetaFetch(async (url) => {
      asked.push(url);
      return url.includes('/docs/')
        ? new Response(
            JSON.stringify({ doc: { slug: 'plan-x1', kind: 'artifact', title: 'План' }, threads: [] }),
          )
        : new Response(JSON.stringify({ kind: 'mr', title: 'MR', state: 'opened', project: 'p', iid: 1 }));
    });
    requestMetas('m1', [
      item('https://app.example.org/agents/docs/plan-x1'),
      item('https://git.example.org/g/p/-/merge_requests/1', ARTIFACT_KIND.mr),
    ]);
    await vi.advanceTimersByTimeAsync(1000);
    expect(asked[0]).toMatch(/\/docs\/plan-x1$/);
    expect(asked[0]).not.toContain('link-meta');
    expect(asked[1]).toContain('link-meta?url=');
    const saved = JSON.parse(localStorage.getItem('hx-next:artifacts:meta:v1') ?? '{}');
    expect(saved['https://app.example.org/agents/docs/plan-x1'].m).toEqual({
      kind: 'artifact',
      title: 'План',
      description: null,
    });
  });
});
