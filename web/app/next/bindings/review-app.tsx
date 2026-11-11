// The built-in «Ревью кода» app on the machine's data, /review: with no change named, its start (the merge requests the
// machine's GitLab reads); `?url=` a merge request or commit read through the runtime's change view; `?chat=` alone the
// work of that chat and the chats it delegated (its worktrees, read on their machines, or a pushed branch from GitLab
// when the machine is off). A chat named by the address stands beside the review where the person put it — a column,
// a window or only its field (on a screen narrower than 1600 the field, until the person moves it).
import { useCallback, useEffect, useState } from 'react';
import ChatView from '@/next/bindings/chat-view';
import { ChatIsland } from '@/next/bindings/chat-island';
import { pageText } from '@/next/bindings/outcome';
import { loadApps } from '@/next/features/apps/api';
import AppMain from '@/next/features/app-main/app-main';
import {
  keepLayout,
  PLACEMENT,
  readLayout,
  withPlacement,
  type IAppLayout,
} from '@/next/features/app-main/placement';
import ReviewHome, { type THomeFeed } from '@/next/features/review/app/review-home';
import type { IGithubList } from '@/next/features/review/app/home';
import { loadGithubPulls } from '@/next/legacy/code-review-api';
import { MrStage, WorkStage } from '@/next/legacy/review-stages';
import { hub } from '@/next/core/realtime/hub';
import type { AppFeed, ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { hasFeature } from '@/next/stores/machines';
import { useConversation, useConversationList } from '@/next/stores/conversations';
import { createConversation, sendMessage } from '@/next/commands';
import { toast } from '@/next/ui/toast';
import { useTabTitle } from '@/next/stores/tabs';
import { RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

const LAYOUT_KEY = 'hx-review-layout';
/** Narrower than this, the chat starts as the field at the foot (the board's «Где чат»). */
const ISLAND_BELOW = 1600;
const CLOCK_MS = 60_000;

const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

const firstLayout = (): IAppLayout =>
  readLayout(LAYOUT_KEY, window.innerWidth < ISLAND_BELOW ? PLACEMENT.island : PLACEMENT.left);

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
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), CLOCK_MS);
    return () => clearInterval(timer);
  }, []);
  return (
    <ReviewHome
      feed={feed}
      now={now}
      onOpen={(url) => goTo({ kind: ROUTE.review, url })}
      onRetry={retry}
      onConnect={() => goTo({ kind: ROUTE.settings, section: 'connections' })}
      onOpenDrawer={onOpenDrawer}
    />
  );
};

// ── the page ──

interface IReviewAppPageProps {
  machine: MachineId;
  url: string | null;
  chat: string | null;
  wt: string | null;
  onOpenDrawer: () => void;
}

const ReviewAppPage = ({ machine: screen, url, chat, wt, onOpenDrawer }: IReviewAppPageProps) => {
  const conv = chat !== null && isConvId(chat) ? chat : null;
  const record = useConversation(conv);
  const machine = record?.machineId ?? screen;
  const [layout, setLayout] = useState<IAppLayout>(firstLayout);
  useTabTitle(
    routePath({ kind: ROUTE.review, ...(url ? { url } : {}), ...(chat ? { chat } : {}) }),
    t('next.reviewApp.title'),
  );
  const place = useCallback((next: IAppLayout) => {
    setLayout(next);
    keepLayout(next, LAYOUT_KEY);
  }, []);

  const onChat = conv !== null && record !== undefined;
  const chats = useConversationList(machine);
  // A question about a merge request goes to the chat beside it; with none, a new chat starts where the latest chat of
  // the machine works, with the question and the link as its first message, and stands beside the review.
  const send = useCallback(
    (message: string) => {
      if (!url) return;
      if (onChat && conv) {
        sendMessage(machine, { conversationId: conv, text: message });
        return;
      }
      const like = chats?.[0];
      if (!like) {
        toast(t('next.appmain.newChatFailed'), { tone: 'error' });
        return;
      }
      void createConversation(machine, {
        where: like.projectId ? { projectId: like.projectId } : { cwd: like.cwd },
        provider: like.provider,
        model: like.model,
        first: { text: message },
      }).conversation.then((made) => {
        if (made) goTo({ kind: ROUTE.review, url, chat: made });
        else toast(t('next.appmain.newChatFailed'), { tone: 'error' });
      });
    },
    [url, onChat, conv, machine, chats],
  );
  const ask = useCallback(
    (text: string) => send(`${t('next.reviewApp.notes.msgQuestion')}\n${url ?? ''}\n\n${text}`),
    [send, url],
  );

  if (!url && !conv) return <Home machine={machine} onOpenDrawer={onOpenDrawer} />;

  const stage = url ? (
    <MrStage url={url} ask={ask} send={send} chat={onChat ? conv : null} machine={machine} />
  ) : (
    conv && <WorkStage machine={machine} chat={conv} wt={wt} />
  );
  return (
    <AppMain
      title={t('next.reviewApp.title')}
      icon="git-pull-request"
      chatTitle={onChat ? (record?.title ?? null) : null}
      chatSwitch={onChat ? <span className="hx-rvapp__subtitle">{record?.title ?? ''}</span> : undefined}
      lead={
        <>
          <button
            type="button"
            className="hx-head-btn hx-rv-menu"
            aria-label={t('next.header.openMenu')}
            onClick={onOpenDrawer}
          >
            <Icon name="menu" />
          </button>
          <RoundButton
            icon="arrow-left"
            label={t('next.reviewApp.toList')}
            size="sm"
            variant="ghost"
            onClick={() => goTo({ kind: ROUTE.review })}
          />
        </>
      }
      placement={layout.placement}
      float={layout.float}
      onPlacement={(placement, section) => place(withPlacement(layout, placement, section))}
      onFloat={(float) => place({ ...layout, float })}
      menu={[]}
      stage={stage}
      chat={
        onChat ? (
          <ChatView
            key={conv}
            conv={conv}
            machine={machine}
            onRetryLoad={() => hub.client(machine)?.reconnectNow()}
            onShowBackground={() => goTo({ kind: ROUTE.chat, id: conv })}
          />
        ) : null
      }
      island={
        onChat ? (
          <ChatIsland
            machine={machine}
            conv={conv}
            onExpand={() => place({ ...layout, placement: PLACEMENT.left })}
          />
        ) : null
      }
      inspector={null}
    />
  );
};

export default ReviewAppPage;
