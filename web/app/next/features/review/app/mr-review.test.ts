import { describe, expect, it } from 'vitest';
import {
  deletableIn,
  draftableIn,
  draftAt,
  FORGE,
  forgeOf,
  forgeOfRemote,
  isReviewLink,
  lineMap,
  openThreadsByFile,
  threadsOf,
  type IMrReview,
} from '@/next/features/review/app/mr-review';

const PATCH = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -3,4 +3,5 @@ func x() {
 c3
-c4
+n4
+n5
 c5
 c6
@@ -20,3 +21,2 @@
 c20
-c21
 c22
`;

describe('lineMap', () => {
  const map = lineMap(PATCH);
  it('pairs untouched lines inside and around the hunks', () => {
    expect(map.oldOf(1)).toBe(1);
    expect(map.oldOf(3)).toBe(3);
    expect(map.oldOf(6)).toBe(5);
    expect(map.oldOf(10)).toBe(9);
    expect(map.oldOf(22)).toBe(22);
    expect(map.oldOf(23)).toBe(23);
    expect(map.oldOf(30)).toBe(30);
    expect(map.newOf(9)).toBe(10);
    expect(map.newOf(22)).toBe(22);
    expect(map.newOf(40)).toBe(40);
  });
  it('has no pair for changed lines', () => {
    expect(map.oldOf(4)).toBeNull();
    expect(map.oldOf(5)).toBeNull();
    expect(map.newOf(4)).toBeNull();
    expect(map.newOf(21)).toBeNull();
  });
});

const refs = { base: 'aaaaaaa', start: 'bbbbbbb', head: 'ccccccc' };

it('places a draft on an untouched line with both numbers, on an added one with its own', () => {
  const file = { path: 'a.go', oldPath: 'a.go', patch: PATCH };
  expect(draftAt(file, 'after', 6, refs, ' why ')).toEqual({
    op: 'draft',
    path: 'a.go',
    oldPath: 'a.go',
    line: 6,
    side: 'after',
    refs,
    body: 'why',
    paired: 5,
  });
  expect(draftAt(file, 'after', 4, refs, 'x').paired).toBeUndefined();
  expect(draftAt(file, 'before', 4, refs, 'x').paired).toBeUndefined();
});

it('shows the current threads of a file and counts the open ones', () => {
  const review = {
    threads: [
      { id: '1', resolvable: true, resolved: false, path: 'a.go', line: 3, side: 'after', notes: [] },
      { id: '2', resolvable: true, resolved: true, path: 'a.go', line: 4, side: 'after', notes: [] },
      {
        id: '3',
        resolvable: true,
        resolved: false,
        path: 'a.go',
        line: 4,
        side: 'after',
        outdated: true,
        notes: [],
      },
      { id: '4', resolvable: false, resolved: false, notes: [] },
    ],
    drafts: [],
  } as unknown as IMrReview;
  expect(threadsOf(review, 'a.go').map((x) => x.id)).toEqual(['1', '2']);
  expect([...openThreadsByFile(review)]).toEqual([['a.go', 2]]);
});

describe('the code host of a review', () => {
  it('tells GitHub from GitLab by the link and by the remote', () => {
    expect(forgeOf('https://github.com/acme/app/pull/7')).toBe(FORGE.github);
    expect(forgeOf('https://gl.example/g/app/-/merge_requests/7')).toBe(FORGE.gitlab);
    expect(isReviewLink('https://github.com/acme/app/pull/7/files')).toBe(true);
    expect(isReviewLink('https://gl.example/g/s/app/-/merge_requests/7')).toBe(true);
    expect(isReviewLink('https://github.com/acme/app/commit/0123abc')).toBe(false);
    expect(isReviewLink('nonsense')).toBe(false);
    expect(forgeOfRemote('git@github.com:acme/app.git')).toBe(FORGE.github);
    expect(forgeOfRemote('https://github.com/acme/app')).toBe(FORGE.github);
    expect(forgeOfRemote('git@gl.example:g/github.git')).toBe(FORGE.gitlab);
  });

  it("keeps GitHub's limits: no draft answer to the conversation, a review's text is not deleted", () => {
    expect(draftableIn(FORGE.github, { path: undefined })).toBe(false);
    expect(draftableIn(FORGE.github, { path: 'a.go' })).toBe(true);
    expect(draftableIn(FORGE.gitlab, { path: undefined })).toBe(true);
    expect(deletableIn(FORGE.github, { id: 'review:202' })).toBe(false);
    expect(deletableIn(FORGE.github, { id: 'PRRT_1' })).toBe(true);
    expect(deletableIn(FORGE.gitlab, { id: 'review:1' })).toBe(true);
  });
});
