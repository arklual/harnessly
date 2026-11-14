import { describe, expect, it } from 'vitest';
import { routeOf, summarize } from '@/next/core/perf-journal';

describe('the page’s minute of performance (HRN-1402)', () => {
  it('says nothing for a quiet minute', () => {
    const m = {
      interactions: new Map(),
      longTasks: 0,
      longTaskMs: 0,
      blockingMs: 0,
      frames: 0,
      frameMs: 0,
      scripts: new Map(),
    };
    expect(summarize(m, { shell: 0, dock: 0, field: 0, feed: 0, sidebar: 0 }, new Map())).toBeNull();
  });

  it('a slow key makes the minute a warn, with what it acted on', () => {
    const m = {
      interactions: new Map([
        [1, { dur: 48, what: 'keydown.hx-composer__input' }],
        [2, { dur: 320, what: 'keydown.hx-composer__input' }],
      ]),
      longTasks: 3,
      longTaskMs: 400,
      blockingMs: 250,
      frames: 2,
      frameMs: 300,
      scripts: new Map([['shared.js:Composer', 210]]),
    };
    const line = summarize(
      m,
      { shell: 1, dock: 0, field: 40, feed: 2, sidebar: 0 },
      new Map([['POST /api/v2/cmd/draft.set', 3]]),
    );
    expect(line?.level).toBe('warn');
    expect(line?.text).toContain('max=320 (keydown.hx-composer__input)');
    expect(line?.text).toContain('field=40');
    expect(line?.text).toContain('shared.js:Composer 210ms');
    expect(line?.text).toContain('POST /api/v2/cmd/draft.set 3');
  });

  it('folds the ids of a route', () => {
    expect(routeOf('GET', '/agents/api/v2/conversations/tab-k3Jx9QmZ/context')).toBe(
      'GET /agents/api/v2/conversations/tab-…/context',
    );
    expect(routeOf('POST', '/agents/api/v2/apps/page-tools')).toBe('POST /agents/api/v2/apps/page-tools');
  });
});
