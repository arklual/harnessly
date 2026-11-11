// A made-up pull request for the gallery of a change's tab (?dev=change&fake=1): the page's fetch answers the change's
// route with it, and a file's sides with «not found». Never part of a production path.
// The shape is the change route's (IChangeView of the legacy code), written out: the gallery imports only @/next/*.

const DIFF = [
  'diff --git a/runtime/changes/registry.go b/runtime/changes/registry.go',
  '--- a/runtime/changes/registry.go',
  '+++ b/runtime/changes/registry.go',
  '@@ -10,7 +10,9 @@ type Registry struct {',
  ' \tmu    sync.Mutex',
  ' \tseen  map[string]time.Time',
  '-\tlimit int',
  '+\t// limit is how many changes one chat keeps before the oldest goes.',
  '+\tlimit int',
  '+\tclock func() time.Time',
  ' }',
  ' ',
  ' func (r *Registry) Add(id string) {',
  'diff --git a/web/app/next/features/review/app/review-app.tsx b/web/app/next/features/review/app/review-app.tsx',
  '--- a/web/app/next/features/review/app/review-app.tsx',
  '+++ b/web/app/next/features/review/app/review-app.tsx',
  '@@ -1,4 +1,5 @@',
  " import { useState } from 'react';",
  "+import { useMemo } from 'react';",
  " import Icon from '@/next/ui/icon';",
  ' ',
  ' export const ReviewApp = () => null;',
].join('\n');

const viewOf = (url: string) => ({
  kind: 'mr',
  provider: url.includes('github') ? 'github' : 'gitlab',
  url,
  host: new URL(url).host,
  project: 'acme/runtime',
  ref: '#12',
  title: 'Keep a clock in the registry of changes',
  description: 'The registry forgets the oldest change after the limit.',
  author: 'alex',
  at: '2026-11-10T10:00:00Z',
  state: 'opened',
  draft: false,
  sourceBranch: 'feat/registry-clock',
  targetBranch: 'main',
  pipeline: { status: 'success', url: `${url}/checks` },
  diff: DIFF,
  truncated: false,
  labels: ['runtime'],
  stages: [],
  comments: [
    {
      id: 'rc-1',
      resolved: false,
      path: 'runtime/changes/registry.go',
      line: 14,
      outdated: false,
      notes: [{ author: 'Oleg', at: '2026-11-10T11:00:00Z', body: 'Why a func and not an interface?' }],
    },
  ],
  limited: null,
});

let faked = false;

export const fakeChangeViews = (url: string) => {
  if (faked) return;
  faked = true;
  const real = window.fetch.bind(window);
  window.fetch = (input, init) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    // A file's both sides are not made up: the diff shows the patch alone.
    if (path.includes('/change-view') && path.includes('path='))
      return Promise.resolve(Response.json({ error: 'none' }, { status: 404 }));
    if (path.includes('/change-view')) return Promise.resolve(Response.json(viewOf(url)));
    return real(input, init);
  };
};
