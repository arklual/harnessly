// The «Ревью кода» app on made-up data, for the eye and for screenshots: ?dev=reviewapp, &view=home|mr|heavy|nogitlab|
// loading|offline|notes|mr|agent|mragent (&asking=1: the agent still going through it; &gl=shared: no own GitLab connection), &place=left|island|float (where the chat stands by an open change), &theme=light|dark, &lang=en|ru.
// Never part of a production path.
import '@/next/styles/order';
import './apps-gallery.css';

import { useEffect, useRef, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import AppMain from '@/next/features/app-main/app-main';
import { PLACEMENTS, type TPlacement } from '@/next/features/app-main/placement';
import ReviewApp from '@/next/features/review/app/review-app';
import type { IGithubList } from '@/next/features/review/app/home';
import ReviewHome, { type THomeFeed } from '@/next/features/review/app/review-home';
import { filesFromPatch, sidesFromPatch } from '@/next/features/review/patch';
import type { IReviewSource, IReviewThread } from '@/next/features/review/model';
import type { IReviewNote } from '@/next/features/review/app/notes';
import type { IReviewNotesApi } from '@/next/features/review/app/use-review-notes';
import { FORGE, type IMrAct, type IMrReview } from '@/next/features/review/app/mr-review';
import type { IMrContext } from '@/next/features/review/app/mr-threads';
import type { IReviewEdit } from '@/next/features/review/app/file-edit';
import { AGENT_STATE, type IAgentReviewApi } from '@/next/features/review/app/use-agent-review';
import type { IAgentSuggestion } from '@/next/features/review/app/agent-review';
import type { AppFeed, AppRow } from '@/next/protocol/v2';
import { Note } from '@/next/features/apps/list';
import { t } from '@/next/i18n';

const NOW = Date.now();
const HOUR = 3_600_000;

const row = (over: Partial<AppRow> & Pick<AppRow, 'id' | 'title'>): AppRow => ({
  key: over.id,
  url: `https://gitlab.example.com/core/app/-/merge_requests/${over.id.slice(1)}`,
  state: 'opened',
  flag: null,
  tone: null,
  project: 'app',
  person: null,
  target: 'main',
  createdAt: NOW - 30 * HOUR,
  updatedAt: NOW - HOUR,
  closedAt: null,
  author: 'Олег Лунин',
  assignees: [],
  reviewers: [],
  me: [],
  labels: [],
  priority: null,
  kind: null,
  parent: null,
  progress: null,
  children: [],
  mr: {
    source: 'feature',
    draft: false,
    conflicts: false,
    needsRebase: false,
    ready: false,
    unresolved: false,
    comments: 0,
    pipeline: 'success',
    approvals: null,
  },
  ...over,
});

const GITLAB: AppFeed = {
  id: 'gitlab',
  state: 'ready',
  reason: null,
  host: 'gitlab.example.com',
  me: 'alex',
  sections: [
    {
      id: 'mrs',
      rows: [
        row({
          id: '!318',
          title: 'Изменения: работа чата и подчатов во вкладке',
          me: ['reviewer'],
          flag: 'review',
          tone: 'accent',
          updatedAt: NOW - HOUR / 1.5,
        }),
        row({
          id: '!311',
          title: 'Сейф: личные секреты в OpenBao',
          author: 'Борис Ким',
          me: ['reviewer'],
          flag: 'review',
          updatedAt: NOW - 26 * HOUR,
        }),
        row({ id: '!412', title: 'Кэш prerender и параллельная сборка витрины', me: ['author'] }),
        row({ id: '!57', title: 'Прокси кэша', project: 'build-cache', me: ['author'] }),
      ],
    },
  ],
} as unknown as AppFeed;

/** The open pull requests of a GitHub beside GitLab's merge requests. */
const GITHUB: IGithubList = {
  state: 'ready',
  reason: null,
  host: 'github.com',
  rows: [
    row({
      id: '#42',
      key: 'gh42',
      url: 'https://github.com/core/sdk/pull/42',
      title: 'Ретраи в клиенте API',
      project: 'core/sdk',
      author: 'Вера Сомова',
      me: ['reviewer'],
      flag: 'review',
      tone: 'accent',
      updatedAt: NOW - HOUR / 3,
    }),
    row({
      id: '#7',
      key: 'gh7',
      url: 'https://github.com/core/docs/pull/7',
      title: 'Раздел про подключения',
      project: 'core/docs',
      me: ['author'],
      updatedAt: NOW - 3 * HOUR,
    }),
  ],
};

const PATCH = `diff --git a/runtime/changes/registry_test.go b/runtime/changes/registry_test.go
new file mode 100644
--- /dev/null
+++ b/runtime/changes/registry_test.go
@@ -0,0 +1,8 @@
+package changes
+
+import "testing"
+
+func TestForChatIncludesChildren(t *testing.T) {
+	r := NewRegistry()
+	r.Record(Work{ChatID: "parent", Repo: "app", Branch: "main"})
+}
diff --git a/runtime/changes/registry.go b/runtime/changes/registry.go
--- a/runtime/changes/registry.go
+++ b/runtime/changes/registry.go
@@ -40,7 +40,12 @@ func (r *Registry) ForChat(id string) []Work {
 	r.mu.Lock()
 	defer r.mu.Unlock()
 	out := r.byChat[id]
-	return out
+	for _, kid := range r.children[id] {
+		out = append(out, r.byChat[kid]...)
+	}
+	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
+	return out
 }
diff --git a/web/app/components/changes-tab.tsx b/web/app/components/changes-tab.tsx
--- a/web/app/components/changes-tab.tsx
+++ b/web/app/components/changes-tab.tsx
@@ -5,6 +5,7 @@ export const ChangesTab = () => {
   const work = useWork();
-  if (!work) return <Empty />;
+  if (work === undefined) return <Skeleton />;
+  if (work === null) return <Empty />;
   return <WorkView work={work} />;
 }
diff --git a/web/pnpm-lock.yaml b/web/pnpm-lock.yaml
--- a/web/pnpm-lock.yaml
+++ b/web/pnpm-lock.yaml
@@ -1,3 +1,3 @@
 lockfileVersion: '9.0'
-  version: 1.0.0
+  version: 1.0.1
`;

const heavyPatch = (): string => {
  const lines = Array.from({ length: 6000 }, (_, i) => `+const line${i} = ${i};`).join('\n');
  return `diff --git a/web/app/surface.gen.ts b/web/app/surface.gen.ts\nnew file mode 100644\n--- /dev/null\n+++ b/web/app/surface.gen.ts\n@@ -0,0 +1,6000 @@\n${lines}\n`;
};

const source = (patch: string): IReviewSource => ({
  key: patch.length.toString(),
  files: filesFromPatch(patch),
  truncated: false,
  meta: {
    kind: 'mr',
    provider: 'gitlab',
    url: 'https://gitlab.example.com/core/app/-/merge_requests/318',
    project: 'core/app',
    ref: '!318',
    title: 'Изменения: работа чата и подчатов во вкладке (HRN-1145)',
    author: 'Олег Лунин',
    sourceBranch: 'agent/hrn-720',
    targetBranch: 'main',
    state: 'opened',
    draft: false,
    pipeline: { status: 'failed', url: null, failedJobUrl: null },
    approvedBy: [],
    approvalsLeft: 1,
    reviewers: ['Андрей'],
    labels: ['runtime'],
    commits: [
      { sha: 'a1b2c3d4e5f6', title: 'feat: registry walks children', author: 'Олег', at: null },
      { sha: 'b2c3d4e5f6a7', title: 'test: children of children', author: 'Олег', at: null },
    ],
    behind: 3,
    conflicts: false,
  },
});

const THREADS: IReviewThread[] = [
  {
    id: '1',
    resolved: false,
    path: 'runtime/changes/registry.go',
    line: 44,
    outdated: false,
    notes: [
      {
        author: 'Борис Ким',
        at: null,
        body: 'Подчаты второго уровня сюда не попадут — обходим только прямых детей.',
      },
      { author: 'Олег Лунин', at: null, body: 'Да, поправлю рекурсией.' },
    ],
  },
  {
    id: '2',
    resolved: true,
    path: null,
    line: null,
    outdated: false,
    notes: [{ author: 'Борис Ким', at: null, body: 'Пайплайн упал на старом моке, не из-за этого MR.' }],
  },
];

const NOTES: IReviewNote[] = [
  {
    id: 'n1',
    path: 'runtime/changes/registry.go',
    side: 'after',
    from: 4,
    to: 6,
    lines: '\tfor _, kid := range r.children[id] {\n\t\tout = append(out, r.byChat[kid]...)\n\t}',
    text: 'Подчаты второго уровня сюда не попадут — обойди детей рекурсивно и добавь тест на внуков.',
    state: 'draft',
    createdAt: NOW - HOUR,
    updatedAt: NOW - HOUR,
  },
  {
    id: 'n2',
    path: 'runtime/changes/registry.go',
    side: 'after',
    from: 7,
    to: 7,
    lines: '\tsort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })',
    text: 'sort.SliceStable, чтобы порядок при равном времени не прыгал.',
    state: 'sent',
    createdAt: NOW - 2 * HOUR,
    updatedAt: NOW - HOUR,
    sentAt: NOW - HOUR,
  },
];

/** The remarks kept in the page only: the hub's answers are made up. */
const useGalleryNotes = (): IReviewNotesApi => {
  const [notes, setNotes] = useState<readonly IReviewNote[]>(NOTES);
  const save = async (note: IReviewNote) => {
    const kept = { ...note, createdAt: note.createdAt || Date.now(), updatedAt: Date.now() };
    setNotes((have) => [...have.filter((n) => n.id !== note.id), kept]);
    return true;
  };
  return {
    state: { k: 'ready', notes },
    save,
    remove: async (id) => {
      setNotes((have) => have.filter((n) => n.id !== id));
      return true;
    },
    send: async () => {
      setNotes((have) =>
        have.map((n) => (n.state === 'draft' ? { ...n, state: 'sent', sentAt: Date.now() } : n)),
      );
      return true;
    },
    ask: () => {},
    retry: () => {},
  };
};

const MR_REVIEW: IMrReview = {
  me: { name: 'Андрей' },
  mine: false,
  title: 'Изменения: работа чата и подчатов во вкладке',
  state: 'opened',
  draft: false,
  approved: false,
  canApprove: true,
  approvedBy: [],
  refs: { base: 'a1b2c3d', start: 'a1b2c3d', head: 'b2c3d4e' },
  threads: [
    {
      id: 't1',
      resolvable: true,
      resolved: false,
      path: 'runtime/changes/registry.go',
      line: 44,
      side: 'after',
      notes: [
        {
          id: 1,
          author: { name: 'Борис Ким' },
          mine: false,
          body: 'Подчаты второго уровня сюда не попадут — обходим только прямых детей.',
        },
        { id: 2, author: { name: 'Олег Лунин' }, mine: false, body: 'Да, поправлю рекурсией.', edited: true },
      ],
    },
    {
      id: 't2',
      resolvable: true,
      resolved: false,
      path: 'runtime/changes/registry.go',
      line: 46,
      side: 'after',
      notes: [
        { id: 3, author: { name: 'Андрей' }, mine: true, body: 'SliceStable, чтобы порядок не прыгал.' },
      ],
    },
    {
      id: 't3',
      resolvable: false,
      resolved: false,
      notes: [
        {
          id: 4,
          author: { name: 'Борис Ким' },
          mine: false,
          body: 'Пайплайн упал на старом моке, не из-за этого MR.',
        },
      ],
    },
  ],
  drafts: [{ id: 9, body: 'Тест на внуков тоже нужен.', replyTo: 't1' }],
};

/** A merge request's review kept in the page: GitLab's answers are made up. */
const useGalleryMr = (personal: boolean, github: boolean): IMrContext => {
  const [inField, setInField] = useState<ReadonlySet<string>>(() => new Set());
  const [review, setReview] = useState<IMrReview>(MR_REVIEW);
  const act = async (a: IMrAct) => {
    setReview((r) => {
      switch (a.op) {
        case 'draft':
          return {
            ...r,
            drafts: [
              ...r.drafts,
              {
                id: Date.now(),
                body: a.body ?? '',
                path: a.path,
                line: a.line,
                side: a.side,
                replyTo: a.thread,
              },
            ],
          };
        case 'draft-delete':
          return { ...r, drafts: r.drafts.filter((d) => d.id !== a.draft) };
        case 'publish':
          return { ...r, drafts: [] };
        case 'approve':
          return { ...r, approved: true };
        case 'unapprove':
          return { ...r, approved: false };
        case 'resolve':
          return {
            ...r,
            threads: r.threads.map((x) => (x.id === a.thread ? { ...x, resolved: Boolean(a.resolved) } : x)),
          };
        default:
          return r;
      }
    });
    return null;
  };
  const url = github
    ? 'https://github.com/core/app/pull/318'
    : 'https://gitlab.example.com/core/app/-/merge_requests/318';
  const connection = {
    id: personal ? 'c1' : 'env-gitlab',
    name: github ? 'GitHub' : 'GitLab',
    personal,
    host: github ? 'github.com' : 'gitlab.example.com',
  };
  return {
    mr: {
      url,
      state: { k: 'ready', connection, review },
      act,
      reload: () => {},
    },
    review,
    writable: personal,
    forge: github ? FORGE.github : FORGE.gitlab,
    onConnect: () => {},
    ask: () => {},
    where: url,
    analyze: galleryAnalyze,
    quoted: { has: (id) => inField.has(id), toggle: (id) => setInField((s) => toggled(s, id)) },
  };
};

const toggled = (set: ReadonlySet<string>, id: string): ReadonlySet<string> => {
  const next = new Set(set);
  if (!next.delete(id)) next.add(id);
  return next;
};

/** The agent's analysis, made up: it answers after a moment, as a side chat would. */
const galleryAnalyze = (question: string, signal: AbortSignal): Promise<string> =>
  new Promise((resolve, reject) => {
    const one = question.includes('{"why"');
    const block = one
      ? {
          why: 'Прав в общем случае: поиск линейный. Но он идёт по записям одного чата — их единицы, у самого «широкого» чата за неделю было 7.',
          replies: [
            'Борис, ищем только в записях одного чата — их единицы, map сейчас не окупится. Зафиксируем допущение комментарием и тестом на 50 записей.',
            'Записей на чат единицы — оставляем линейный поиск, допишем тест на 50.',
          ],
        }
      : {
          points: [
            {
              who: 'Борис Ким',
              why: 'Подчаты второго уровня действительно не попадут: обход только прямых детей.',
            },
            { who: 'Олег Лунин', why: 'Обещал рекурсию — в MR её пока нет.' },
          ],
          sum: 'Блокер одобрения: без обхода внуков вкладка теряет работу подчатов.',
          replies: [
            'Олег, жду рекурсию и тест на внуков — тогда одобрю.',
            'Без обхода внуков не одобряю: это требование спеки.',
          ],
        };
    const timer = window.setTimeout(
      () => resolve(`\`\`\`review-analysis\n${JSON.stringify(block)}\n\`\`\``),
      1200,
    );
    signal.addEventListener('abort', () => {
      window.clearTimeout(timer);
      reject(new Error('aborted'));
    });
  });

const first = (v: string | string[] | undefined): string | undefined => (Array.isArray(v) ? v[0] : v);

/** A chat's own work: both whole sides (rebuilt from the patch), so the new side can be edited. */
const localSource = (patch: string): IReviewSource => ({
  key: `local${patch.length}`,
  files: filesFromPatch(patch),
  truncated: false,
  loadContent: (file) => Promise.resolve({ ...sidesFromPatch(file.patch), complete: true }),
  meta: {
    kind: 'local',
    repo: 'app',
    root: '/repo/.worktrees/hrn-720',
    branch: 'agent/hrn-720',
    base: 'HEAD',
    commits: [{ sha: 'a1b2c3d4e5f6', title: 'feat: registry walks children', author: 'agent', at: null }],
    uncommitted: true,
  },
});

const GALLERY_EDIT: IReviewEdit = {
  save: (path, text) =>
    Promise.resolve({ path, content: text, revision: String(text.length), bytes: text.length }),
  commit: () => Promise.resolve('c0ffee12d34'),
  hash: (text) => Promise.resolve(String(text.length)),
  reload: () => {},
  askCommit: () => {},
};

const SUGGESTIONS: IAgentSuggestion[] = [
  {
    id: 'ag1',
    path: 'runtime/changes/registry.go',
    side: 'after',
    from: 42,
    to: 42,
    text: 'out делит массив с r.byChat[id]: append допишет в него и испортит реестр. Скопируйте срез.',
    replacement: '\tout := append([]Work(nil), r.byChat[id]...)',
  },
  {
    id: 'ag2',
    path: 'web/app/components/changes-tab.tsx',
    side: 'after',
    from: 7,
    to: 7,
    text: 'Скелетон без подписи — добавьте aria-busy, чтобы читалка сказала «загрузка».',
  },
];

/** The agent's pass kept in the page: its answer is made up. */
const useGalleryAgent = (asking: boolean): IAgentReviewApi => {
  const [gone, setGone] = useState<ReadonlySet<string>>(() => new Set());
  const suggestions = SUGGESTIONS.filter((s) => !gone.has(s.id));
  return {
    state: asking ? AGENT_STATE.asking : AGENT_STATE.idle,
    suggestions,
    done: suggestions.length === 0,
    ask: () => {},
    settle: (id) => setGone((have) => new Set(have).add(id)),
  };
};

const ReviewAppGallery = () => {
  const { query: params } = useRouter();
  const dark = params.theme === 'dark';
  pinLocale(params.lang === 'en' ? 'en' : 'ru');
  const view = first(params.view) ?? 'home';
  const [placement, setPlacement] = useState<TPlacement>(
    PLACEMENTS.find((p) => p === first(params.place)) ?? 'island',
  );
  const portal = useRef<HTMLDivElement>(null);
  const notes = useGalleryNotes();
  const mr = useGalleryMr(params.gl !== 'shared', params.host === 'github');
  const agent = useGalleryAgent(params.asking === '1');
  const own = view === 'notes' || view === 'agent';
  const withAgent = view === 'agent' || view === 'mragent';
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);

  const homeFeed = ((): THomeFeed => {
    if (view === 'loading') return { k: 'loading' };
    if (view === 'offline') return { k: 'failed', text: t('next.reviewApp.offlineText') };
    const github = ((): IGithubList | null => {
      if (params.host === 'github') return GITHUB;
      if (params.host === 'ghdown')
        return { state: 'failed', reason: 'unreachable', host: 'github.com', rows: [] };
      return null;
    })();
    if (view === 'nogitlab') return { k: 'ready', gitlab: null, github, waiting: false };
    return { k: 'ready', gitlab: GITLAB, github, waiting: false };
  })();
  const home = view === 'home' || view === 'loading' || view === 'offline' || view === 'nogitlab';

  const stage =
    view === 'machineoff' ? (
      <Note
        icon="wifi-off"
        title={t('next.reviewApp.machineOff')}
        text={t('next.reviewApp.machineOffText')}
      />
    ) : (
      <ReviewApp
        source={
          view === 'agent' ? localSource(PATCH) : source(view === 'heavy' ? heavyPatch() + PATCH : PATCH)
        }
        threads={own ? null : THREADS}
        notes={own ? notes : null}
        mr={view === 'mr' || view === 'mragent' ? mr : null}
        worktree={own ? '/repo/.worktrees/hrn-720' : undefined}
        edit={view === 'agent' ? GALLERY_EDIT : null}
        agent={withAgent ? agent : null}
        actions={
          <a className="hx-rvapp__out" href="https://gitlab.example.com">
            GitLab <span aria-hidden="true">↗</span>
          </a>
        }
      />
    );

  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-gallery-apps">
        <main className="hx-main">
          {home ? (
            <ReviewHome
              feed={homeFeed}
              now={NOW}
              onOpen={() => {}}
              onRetry={() => {}}
              onConnect={() => {}}
              onOpenDrawer={() => {}}
            />
          ) : (
            <AppMain
              title={t('next.reviewApp.title')}
              icon="git-pull-request"
              chatTitle="Посмотри !318 Ромы"
              placement={placement}
              float={null}
              onPlacement={setPlacement}
              onFloat={() => {}}
              menu={[]}
              stage={stage}
              chat={<p className="hx-rvhome__empty">Посмотрел все 8 файлов и сверил с HRN-1145.</p>}
              island={<p className="hx-rvhome__empty">Спросить про этот MR</p>}
              inspector={null}
            />
          )}
        </main>
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

export default ReviewAppGallery;
