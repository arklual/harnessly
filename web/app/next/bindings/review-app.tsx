// The built-in «Ревью кода» app on the machine's data, /review: with no change named, its start (the merge requests the
// machine's GitLab reads); `?url=` a merge request or commit read through the runtime's change view; `?chat=` alone the
// work of that chat and the chats it delegated (its worktrees, read on their machines, or a pushed branch from GitLab
// when the machine is off). It stands in Sinopia's stage (app-stage.tsx): the chat beside the review is the stage's, at
// the left or the right, in a window or only its field, with the choice of the chat, kept for the review.
import { useCallback, useEffect, useMemo, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import { loadApps } from '@/next/features/apps/api';
import AppStage from '@/next/features/app-main/app-stage';
import { startFolder } from '@/next/bindings/home-composer';
import { useSiteStage } from '@/next/bindings/site-stage';
import type { TChatOpeners } from '@/next/bindings/chat-view';
import { useDefaultCwd } from '@/next/stores/projects';
import { useReviewRules } from '@/next/legacy/review-rules';
import ReviewHome, { type THomeFeed } from '@/next/features/review/app/review-home';
import type { IGithubList } from '@/next/features/review/app/home';
import { loadGithubPulls } from '@/next/legacy/code-review-api';
import { changeTabName } from '@/next/legacy/change';
import { MrStage, WorkStage } from '@/next/legacy/review-stages';
import type { AppFeed, ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { hasFeature } from '@/next/stores/machines';
import { useConversation, useConversationList } from '@/next/stores/conversations';
import { useTabTitle } from '@/next/stores/tabs';
import { t } from '@/next/i18n';

const CLOCK_MS = 60_000;

const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

// ── the start ──

type TGitlabRead = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'done'; app: AppFeed | null };
type TGithubRead = { k: 'loading' } | { k: 'done'; list: IGithubList };

const READING = { gitlab: { k: 'loading' }, github: { k: 'loading' } } as const;

/** The start's lists as far as they are read: whichever source answers first is shown, the other joins it. */
const homeFeedOf = (gitlab: TGitlabRead, github: TGithubRead): THomeFeed => {
  const list = github.k === 'done' ? github.list : null;
  if (gitlab.k === 'done')
    return { k: 'ready', gitlab: gitlab.app, github: list, waiting: github.k === 'loading' };
  if (list?.state === 'ready')
    return { k: 'ready', gitlab: null, github: list, waiting: gitlab.k === 'loading' };
  if (gitlab.k === 'failed' && github.k === 'done') return { k: 'failed', text: gitlab.text };
  return { k: 'loading' };
};

const useHomeFeed = (machine: MachineId) => {
  const supported = hasFeature(machine, 'apps-hub');
  const [reads, setReads] = useState<{ gitlab: TGitlabRead; github: TGithubRead }>(READING);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!supported) return;
    let live = true;
    // GitLab and GitHub are read apart: a slow or silent one does not hold the other's lists back.
    void loadApps(machine, ['gitlab']).then((res) => {
      if (!live) return;
      const gitlab: TGitlabRead =
        'ok' in res
          ? { k: 'done', app: res.value.apps.find((a) => a.id === 'gitlab') ?? null }
          : { k: 'failed', text: pageText(res) };
      setReads((was) => ({ ...was, gitlab }));
    });
    void loadGithubPulls().then((list) => {
      if (live) setReads((was) => ({ ...was, github: { k: 'done', list } }));
    });
    return () => {
      live = false;
    };
  }, [machine, supported, attempt]);
  const retry = useCallback(() => {
    setReads(READING);
    setAttempt((n) => n + 1);
  }, []);
  const feed: THomeFeed = supported
    ? homeFeedOf(reads.gitlab, reads.github)
    : { k: 'ready', gitlab: null, github: null, waiting: false };
  return { feed, retry };
};

const Home = ({ machine, onOpenDrawer }: { machine: MachineId; onOpenDrawer: () => void }) => {
  const { feed, retry } = useHomeFeed(machine);
  const rules = useReviewRules();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), CLOCK_MS);
    return () => clearInterval(timer);
  }, []);
  return (
    <ReviewHome
      feed={feed}
      rules={rules}
      now={now}
      onOpen={(url) => goTo({ kind: ROUTE.review, url })}
      onRetry={retry}
      onConnect={() => goTo({ kind: ROUTE.settings, section: 'connections' })}
      onOpenDrawer={onOpenDrawer}
    />
  );
};

// ── the page ──

export interface IReviewAppPageProps {
  machine: MachineId;
  url: string | null;
  chat: string | null;
  wt: string | null;
  onOpenDrawer: () => void;
  open?: TChatOpeners;
  /** The right pane of the chat, as beside a design. */
  toolPane?: { open: boolean; onToggle: () => void } | null;
}

/** The review on the whole window in Sinopia's stage: the chat, its place and its switch are the stage's. */
const ReviewStagePage = ({
  machine: screen,
  url,
  chat,
  wt,
  onOpenDrawer,
  open,
  toolPane = null,
}: IReviewAppPageProps) => {
  const conv = chat !== null && isConvId(chat) ? chat : null;
  const record = useConversation(conv);
  const machine = record?.machineId ?? screen;
  const chats = useConversationList(machine);
  const defaultCwd = useDefaultCwd(machine);
  useTabTitle(
    routePath({ kind: ROUTE.review, ...(url ? { url } : {}), ...(chat ? { chat } : {}) }),
    t('next.reviewApp.title'),
  );
  const [read, setRead] = useState<{ url: string; title: string; crumb: string } | null>(null);
  const onRead = useCallback(
    (info: { title: string; crumb: string }) => {
      if (url) setRead({ url, ...info });
    },
    [url],
  );
  const named = url && read?.url === url ? read : null;
  const title = named?.title ?? (url ? changeTabName(url) : (record?.title ?? t('next.reviewApp.workTitle')));
  const address = useMemo(
    () => ({ kind: ROUTE.review, ...(url ? { url } : {}), ...(wt ? { wt } : {}) }) as const,
    [url, wt],
  );
  const stage = useSiteStage({
    machine,
    design: url ? `review:${url}` : `review:chat:${conv ?? ''}`,
    fromChat: conv,
    dir: record?.cwd ?? startFolder(chats, machine, defaultCwd ?? null),
    tied: [],
    title,
    pageUrl: '',
    port: null,
    open,
    keep: true,
    app: { address, onCreated: () => undefined },
  });
  const onChat = stage.conv ?? null;
  const send = useCallback((message: string) => stage.ask?.(message), [stage]);
  const ask = useCallback(
    (text: string) => send(`${t('next.reviewApp.notes.msgQuestion')}\n${url ?? ''}\n\n${text}`),
    [send, url],
  );
  const frame = url ? (
    <MrStage url={url} ask={ask} send={send} chat={onChat} machine={machine} onRead={onRead} />
  ) : (
    conv && <WorkStage machine={machine} chat={conv} wt={wt} />
  );
  return (
    <AppStage
      title={title}
      crumb={named?.crumb}
      // Back goes where the person came from: the chat the review stands beside, else the start.
      onBack={conv ? undefined : () => goTo({ kind: ROUTE.review })}
      backLabel={t('next.reviewApp.toList')}
      onCollapse={conv ? () => goTo({ kind: ROUTE.chat, id: conv }) : undefined}
      stage={stage}
      frame={frame}
      inspector={null}
      menu={[]}
      toolPane={toolPane}
      onOpenDrawer={onOpenDrawer}
    />
  );
};

const ReviewAppPage = (props: IReviewAppPageProps) => {
  const conv = props.chat !== null && isConvId(props.chat) ? props.chat : null;
  if (!props.url && !conv) return <Home machine={props.machine} onOpenDrawer={props.onOpenDrawer} />;
  return <ReviewStagePage {...props} />;
};

export default ReviewAppPage;
