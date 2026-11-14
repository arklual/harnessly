import { describe, expect, it } from 'vitest';
import { LINK, STATE_TONE, linkOf, viewOf } from '@/next/features/chat/link-output';
import { LOOK } from '@/next/features/artifacts/model';
import type { TurnOutput } from '@/next/protocol/v2';

const config = {
  artifactsUrl: 'https://a.example.com',
  boardsUrl: 'https://a.example.com',
  readerUrl: null,
  githubHost: 'github.com',
};
const out = (kind: TurnOutput['kind'], ref: string, title = 'x'): TurnOutput => ({
  kind,
  op: 'created',
  ref,
  title,
});

describe('linkOf', () => {
  it('tells every service by the kind and the address', () => {
    const cases: [TurnOutput, string, string][] = [
      [
        out('stand', 'https://stand-1.preview.example.com/adminpanel'),
        LINK.stand,
        'stand-1.preview.example.com',
      ],
      [out('stand', 'http://localhost:4400/x'), LINK.server, 'localhost:4400'],
      [
        out('mr', 'https://gitlab.example.com/g/harnessly/-/merge_requests/80', 'harnessly !80'),
        LINK.mr,
        'harnessly !80',
      ],
      [out('mr', 'https://github.com/o/r/pull/7', 'r #7'), LINK.pr, 'r #7'],
      [
        out('commit', 'https://gitlab.example.com/g/harnessly/-/commit/8ebd61a76b62'),
        LINK.commit,
        'harnessly 8ebd61a7',
      ],
      [out('task', 'https://tracker.example.com/issue/HRN-1426'), LINK.task, 'HRN-1426'],
      [out('artifact', 'https://a.example.com/a/plan'), LINK.doc, 'a.example.com'],
      [out('artifact', 'https://a.example.com/b/flow'), LINK.board, 'a.example.com'],
      [out('artifact', 'https://claude.ai/artifact/abc'), LINK.page, 'claude.ai'],
    ];
    for (const [o, kind, key] of cases) expect(linkOf(o, config), o.ref).toMatchObject({ kind, key });
  });

  it('is no link for a design or a file, and a dev server has no new tab', () => {
    expect(linkOf(out('design', 'my-design'), config)).toBeNull();
    expect(linkOf(out('doc', 'docs/plan.md'), config)).toBeNull();
    expect(linkOf(out('stand', 'http://localhost:4400/'), config)).toMatchObject({
      newTab: false,
      port: 4400,
      item: null,
    });
  });

  it('reads a GitHub pull request by the GitHub route and leaves a GitHub commit unread', () => {
    expect(linkOf(out('mr', 'https://github.com/o/r/pull/7'), config)?.item?.github).toBe(true);
    expect(linkOf(out('commit', 'https://github.com/o/r/commit/8ebd61a'), config)?.item?.readable).toBe(
      false,
    );
  });
});

describe('viewOf', () => {
  const mr = linkOf(
    out('mr', 'https://gitlab.example.com/g/harnessly/-/merge_requests/80', 'harnessly !80'),
    config,
  )!;
  it('a merge request says its state and its pipeline', () => {
    const v = viewOf(
      mr,
      {
        kind: 'mr',
        title: 'Карточки',
        state: 'opened',
        project: 'g/harnessly',
        iid: 80,
        draft: false,
        pipeline: 'failed',
      },
      LOOK.got,
      null,
    );
    expect(v.title).toBe('Карточки');
    expect(v.tone).toBe(STATE_TONE.bad);
    expect(v.meta).toContain('harnessly !80');
    const merged = viewOf(
      mr,
      {
        kind: 'mr',
        title: 'Карточки',
        state: 'merged',
        project: 'g/harnessly',
        iid: 80,
        draft: false,
        pipeline: 'failed',
      },
      LOOK.got,
      null,
    );
    expect(merged.tone).toBe(STATE_TONE.rest);
  });

  it('a task says its state and who has it', () => {
    const task = linkOf(out('task', 'https://tracker.example.com/issue/HRN-1426'), config)!;
    const v = viewOf(
      task,
      {
        kind: 'issue',
        key: 'HRN-1426',
        title: 'Карточки ссылок',
        state: 'In Progress',
        resolved: false,
        assignee: { name: 'Анна', avatar: null },
      },
      LOOK.got,
      null,
    );
    expect(v).toMatchObject({
      title: 'Карточки ссылок',
      meta: 'HRN-1426 · In Progress · Анна',
      tone: STATE_TONE.busy,
    });
  });

  it('a stand and a dev server say whether they answer', () => {
    const stand = linkOf(out('stand', 'https://stand-1.preview.example.com/'), config)!;
    expect(viewOf(stand, { kind: 'stand', up: false, title: null }, LOOK.got, null).tone).toBe(
      STATE_TONE.bad,
    );
    expect(viewOf(stand, { kind: 'stand', up: true, title: 'Админка' }, LOOK.got, null)).toMatchObject({
      title: 'Админка',
      tone: STATE_TONE.ok,
    });
    const server = linkOf(out('stand', 'http://localhost:4400/'), config)!;
    expect(viewOf(server, null, LOOK.none, true).tone).toBe(STATE_TONE.ok);
    expect(viewOf(server, null, LOOK.none, null).tone).toBeNull();
  });

  it('while the state is read, the card shows the turn its own and says it softly', () => {
    expect(viewOf(mr, null, LOOK.wait, null)).toMatchObject({ title: null, pending: true, tone: null });
  });
});
