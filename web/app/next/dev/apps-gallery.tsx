// The Apps screen on made-up data, for the eye and for screenshots: ?dev=apps with the installation's next-ui
// feature. &app=tracker|gitlab picks the open app, &page=0 leaves the list over the page (a phone), &state=ready|off|
// failed|loading|error the machine's answer, &picked=2 picks that many rows, &theme=light|dark and &lang=en|ru pick the
// look. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/icons.css';
import './apps-gallery.css';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from '@/platform/router';
import { pinLocale } from '@/next/i18n';
import AppsView from '@/next/features/apps/apps-view';
import { PortalContext } from '@/next/ui/portal';
import { rowsOf } from '@/next/features/apps/model';
import type { AppChild, AppFeed, AppId, AppMR, AppPipeline, AppRow, AppsFeed } from '@/next/protocol/v2';

const DAY = 86_400_000;
const HOUR = 3_600_000;
const NOW = Date.now();
const TRACKER = 'https://tracker.example.com/issue/';
const GITLAB = 'https://gitlab.example.com/team/';

const PEOPLE = ['Peter S.', 'Ivan K.', 'Oleg N.', 'Anna V.'] as const;
const STATES = ['New', 'In Progress', 'Testing', 'Done'] as const;
const TONE_OF = { New: null, 'In Progress': 'accent', Testing: 'warn', Done: null } as const;
const EPIC_NAMES = [
  'Apps screen with the left picker',
  'Refund documents for organizers',
  'Scanner offline trust',
  'Order pay page observability',
  'Admin analytics dashboard',
  'Storefront bundle budget',
  'Machine agent sandbox',
  'Marketing links redesign',
  'Platform revenue reconciliation',
  'Chat feed performance',
  'Design library sharing',
  'Release pipeline gates',
] as const;
const TASK_NAMES = [
  'Fix the cart total after a promo code',
  'Renew the TLS certificate of the preview stand',
  'Add the filter by tag to the tracker list',
  'Show the pipeline status in the merge request row',
  'Move the metrics collect contract to a batch of 50',
  'Write the e2e for the payment form events',
  'Check the iOS 17 install prompt',
  'Clean the stale runners',
  'Document the worktree policy',
  'Rename the inventory availability field',
] as const;
const LABELS = [
  ['repo:admin'],
  ['repo:web', 'waiting:external'],
  [],
  ['repo:runtime'],
  ['repo:admin', 'quality', 'ux'],
] as const;

const kid = (id: number, n: number): AppChild => {
  const done = n % 3 === 0;
  return {
    id: `ABC-${id}`,
    title: `${['Write the schema', 'Wire the screen', 'Cover with tests', 'Review the copy', 'Roll out', 'Measure'][n % 6]} (${n + 1})`,
    url: `${TRACKER}ABC-${id}`,
    state: done ? 'Done' : n % 3 === 1 ? 'In Progress' : 'New',
    done,
    person: n % 2 === 0 ? PEOPLE[n % PEOPLE.length]! : null,
  };
};

const epics = (): AppRow[] =>
  EPIC_NAMES.map((title, index) => {
    const id = 2600 + index * 7;
    const total = index === 11 ? 0 : 3 + (index % 5);
    const children = Array.from({ length: total }, (_, n) => kid(id + 100 + n, n + index));
    const done = children.filter((one) => one.done).length;
    const state = STATES[index % 3]!;
    return {
      key: `ABC-${id}`,
      id: `ABC-${id}`,
      title,
      url: `${TRACKER}ABC-${id}`,
      state,
      flag: null,
      tone: TONE_OF[state],
      project: 'ABC',
      person: PEOPLE[index % PEOPLE.length]!,
      target: null,
      createdAt: NOW - (60 + index * 4) * DAY,
      updatedAt: NOW - (index % 5 === 4 ? 9 : index) * DAY - index * HOUR,
      closedAt: null,
      author: PEOPLE[0],
      assignees: [PEOPLE[index % PEOPLE.length]!],
      reviewers: [],
      me: ['assignee'],
      labels: [...LABELS[index % LABELS.length]!],
      priority: index % 4 === 0 ? 'Major' : 'Normal',
      kind: 'Epic',
      parent: null,
      progress: { done, total },
      children,
      mr: null,
    } satisfies AppRow;
  });

const tasks = (): AppRow[] =>
  Array.from({ length: 28 }, (_, index) => {
    const id = 3000 + index * 3;
    const ops = index % 9 === 4;
    const state = ops ? 'В работе' : STATES[index % 3]!;
    const parent = index % 3 === 0 ? undefined : EPIC_NAMES[index % EPIC_NAMES.length]!;
    const epicId = 2600 + (index % EPIC_NAMES.length) * 7;
    return {
      key: ops ? `OPS-${id}` : `ABC-${id}`,
      id: ops ? `OPS-${id}` : `ABC-${id}`,
      title: TASK_NAMES[index % TASK_NAMES.length]!,
      url: `${TRACKER}ABC-${id}`,
      state,
      flag: null,
      tone: ops ? 'accent' : TONE_OF[STATES[index % 3]!],
      project: ops ? 'OPS' : 'ABC',
      person: PEOPLE[0],
      target: null,
      createdAt: NOW - (20 + index) * DAY,
      updatedAt: NOW - (index % 7 === 6 ? 8 : index % 5) * DAY - index * HOUR,
      closedAt: null,
      author: PEOPLE[1],
      assignees: [PEOPLE[0]],
      reviewers: [],
      me: ['assignee'],
      labels: [...LABELS[index % LABELS.length]!],
      priority: index % 6 === 0 ? 'Critical' : 'Normal',
      kind: ops ? null : (['Bug', 'Feature', 'Engineering'][index % 3] ?? null),
      parent: parent ? { id: `ABC-${epicId}`, title: parent, url: `${TRACKER}ABC-${epicId}` } : null,
      progress: null,
      children: [],
      mr: null,
    } satisfies AppRow;
  });

const PIPELINES: readonly (AppPipeline | null)[] = [
  'success',
  'failed',
  'running',
  null,
  'success',
  'pending',
  'canceled',
];
const MR_NAMES = [
  'Apps screen: the left picker',
  'Fix the refund total rounding',
  'Scanner: trust the offline queue',
  'Draft: bundle budget for the lazy route',
  'Bump the runtime image',
  'Analytics: bar lists instead of tables',
  'Chat feed: do not remount on probes',
  'Release guard: record the last green main',
  'Add the T-Pay iframe guard',
  'Metrics: batch of 50',
  'Design library ACL',
  'Marketing links: names on the link',
] as const;

const mrs = (): AppRow[] =>
  MR_NAMES.map((title, index) => {
    const state = index === 9 ? 'merged' : index === 10 ? 'closed' : 'opened';
    const open = state === 'opened';
    const pipeline = open ? PIPELINES[index % PIPELINES.length]! : null;
    const mr: AppMR = {
      source: ['feature/apps-left', 'fix/refund-round', 'scanner/offline', 'perf/budget', 'chore/image'][
        index % 5
      ]!,
      draft: index === 3,
      conflicts: index === 1,
      needsRebase: index === 4,
      ready: pipeline === 'success' && index !== 1,
      unresolved: index === 5,
      comments: index % 4 === 0 ? 0 : index,
      pipeline,
      approvals: index % 3 === 0 ? { given: index % 2, required: 1 } : null,
    };
    return {
      key: `!${100 + index}`,
      id: `!${100 + index}`,
      title,
      url: `${GITLAB}api/-/merge_requests/${100 + index}`,
      state,
      flag: index === 0 ? 'review' : index === 1 ? 'conflict' : index === 2 ? 'ci-failed' : null,
      tone: index === 1 ? 'warn' : index === 0 ? 'accent' : null,
      project: ['harnessly', 'payments', 'storefront', 'acme-design'][index % 4]!,
      person: PEOPLE[index % PEOPLE.length]!,
      target: 'main',
      createdAt: NOW - (3 + index) * DAY,
      updatedAt: NOW - (index % 6 === 5 ? 7 : index % 3) * DAY - index * HOUR,
      closedAt: open ? null : NOW - DAY,
      author: PEOPLE[index % PEOPLE.length]!,
      assignees: index % 2 === 0 ? [PEOPLE[0]] : [],
      reviewers: index % 3 === 0 ? [PEOPLE[0]] : [PEOPLE[2]],
      me: index === 0 ? ['reviewer'] : index % 4 === 1 ? ['author', 'assignee'] : [],
      labels: index % 3 === 2 ? ['backend', 'quality'] : [],
      priority: null,
      kind: null,
      parent: null,
      progress: null,
      children: [],
      mr,
    } satisfies AppRow;
  });

type TState = 'ready' | 'off' | 'failed' | 'loading' | 'error';

const feedOf = (state: TState): AppsFeed => {
  const broken = (id: AppId, host: string, reason: 'no-token' | 'unreachable'): AppFeed => ({
    id,
    state: reason === 'no-token' ? 'off' : 'failed',
    reason,
    host,
    me: null,
    sections: [],
  });
  const tracker: AppFeed = {
    id: 'tracker',
    state: 'ready',
    reason: null,
    host: 'tracker.example.com',
    me: 'p.smirnov',
    sections: [
      { id: 'epics', rows: epics() },
      { id: 'tasks', rows: tasks() },
    ],
  };
  const gitlab: AppFeed = {
    id: 'gitlab',
    state: 'ready',
    reason: null,
    host: 'gitlab.example.com',
    me: 'p.smirnov',
    sections: [{ id: 'mrs', rows: mrs() }],
  };
  if (state === 'off')
    return { at: NOW, staleDays: 5, apps: [tracker, broken('gitlab', 'gitlab.example.com', 'no-token')] };
  if (state === 'failed')
    return { at: NOW, staleDays: 5, apps: [tracker, broken('gitlab', 'gitlab.example.com', 'unreachable')] };
  return { at: NOW, staleDays: 5, apps: [tracker, gitlab] };
};

const AppsGallery = () => {
  const { query } = useRouter();
  const dark = query.theme === 'dark';
  pinLocale(query.lang === 'ru' ? 'ru' : 'en');
  const state: TState =
    (['off', 'failed', 'loading', 'error'] as const).find((one) => one === query.state) ?? 'ready';
  const [app, setApp] = useState<AppId>(query.app === 'gitlab' ? 'gitlab' : 'tracker');
  const [pageOpen, setPageOpen] = useState(query.page !== '0');
  const feed = useMemo(() => (state === 'loading' || state === 'error' ? null : feedOf(state)), [state]);
  const portal = useRef<HTMLDivElement>(null);
  const wanted = Number(query.picked ?? 0);
  const [picked, setPicked] = useState<ReadonlySet<string>>(() => {
    const open = feed?.apps.find((one) => one.id === app);
    return new Set(
      open
        ? rowsOf(open)
            .slice(0, wanted)
            .map((row) => row.key)
        : [],
    );
  });
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.theme = dark ? 'dark' : 'light';
    root.classList.toggle('dark', dark);
    root.style.colorScheme = dark ? 'dark' : 'light';
  }, [dark]);
  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-gallery-apps">
        <main className="hx-main">
          <AppsView
            feed={feed}
            error={state === 'error' ? 'The machine did not answer in time' : null}
            refreshing={false}
            app={app}
            onApp={(next) => {
              setApp(next);
              setPageOpen(true);
              setPicked(new Set());
            }}
            pageOpen={pageOpen}
            onBack={() => setPageOpen(false)}
            picked={picked}
            onToggle={(key) =>
              setPicked((current) => {
                const next = new Set(current);
                if (!next.delete(key)) next.add(key);
                return next;
              })
            }
            onClear={() => setPicked(new Set())}
            onChat={() => {}}
            onRetry={() => {}}
            onOpenDrawer={() => {}}
            onSettings={() => {}}
            now={NOW}
          />
        </main>
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

export default AppsGallery;
