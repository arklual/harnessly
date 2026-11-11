// The stages of the «Ревью кода» app over the current interface's data (the bridge, as local-review.tsx): a merge
// request or commit read through the runtime's change view, and the work of a chat — its worktrees and its delegated
// chats', read on their machines, or a pushed branch from GitLab when the machine is off.
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { appFetch } from '@/lib/app-path';
import {
  fetchChatWork,
  fetchMachineChanges,
  fetchRemoteChanges,
  groupWork,
  mrLabel,
  type IChangeSet,
  type IChatWork,
  type IWorkEntry,
} from '@/lib/chat-work';
import type { IChangeView } from '@/lib/hl-gitlab-ref';
import { Note, PageSkeleton } from '@/next/features/apps/list';
import ReviewApp from '@/next/features/review/app/review-app';
import { WorkPick, type IWorkOption } from '@/next/features/review/app/work-pick';
import { useReviewNotes } from '@/next/features/review/app/use-review-notes';
import { forgeOf, FORGE, isReviewLink } from '@/next/features/review/app/mr-review';
import { useMrContext, useMrReview, type IMrReviewIo } from '@/next/features/review/app/use-mr-review';
import { CreateMr, OwnMr, toConnections } from '@/next/legacy/own-mr';
import { actMrReview, loadMrReview } from '@/next/legacy/code-review-api';
import { chatAnswers, contentHash, sideAnswer, workWriter } from '@/next/legacy/work-write-api';
import { putQuote, removeQuote, useQuotes } from '@/next/stores/quotes';
import type { IQuoted } from '@/next/features/review/app/mr-threads';
import { useAgentReview, type IAgentReviewIo } from '@/next/features/review/app/use-agent-review';
import type { IReviewEdit } from '@/next/features/review/app/file-edit';
import { sendMessage } from '@/next/commands/messages';
import type { IReviewSource } from '@/next/features/review/model';
import { changeSetSource, changeViewSource } from '@/next/legacy/review-sources';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { Button } from '@/next/ui/button';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';

// ── a merge request or a commit ──

type TView = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; view: IChangeView };

const loadView = async (url: string): Promise<TView> => {
  try {
    const response = await appFetch(`/api/platform/change-view?${new URLSearchParams({ url })}`);
    if (response.ok) return { k: 'ready', view: (await response.json()) as IChangeView };
    const said = await response
      .json()
      .then((body: { error?: unknown }) => (typeof body?.error === 'string' ? body.error : null))
      .catch(() => null);
    return { k: 'failed', text: said ?? t('next.reviewApp.httpError', { status: response.status }) };
  } catch {
    return { k: 'failed', text: t('next.reviewApp.offlineText') };
  }
};

const useChangeView = (url: string) => {
  const [state, setState] = useState<{ url: string; view: TView } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    void loadView(url).then((view) => {
      if (live) setState({ url, view });
    });
    return () => {
      live = false;
    };
  }, [url, attempt]);
  const view: TView = state?.url === url ? state.view : { k: 'loading' };
  return { view, retry: () => setAttempt((n) => n + 1) };
};

const OpenLink = ({ url, provider }: { url: string; provider: string }) => (
  <a className="hx-rvapp__out" href={url} target="_blank" rel="noopener noreferrer">
    {provider === 'github' ? 'GitHub' : 'GitLab'}
    <span aria-hidden="true">↗</span>
  </a>
);

/** The code host's review of a merge request (pull request) link; anything else has none. */
const MR_IO: IMrReviewIo = {
  load: (url) =>
    isReviewLink(url) ? loadMrReview(url) : Promise.resolve({ ok: false, refusal: { k: 'no-connection' } }),
  act: actMrReview,
};

const NO_ANSWERS: IAgentReviewIo['read'] = () => Promise.resolve([]);

interface IMrStageProps {
  url: string;
  /** A question about lines, to the agent beside (or a new chat). */
  ask?: (text: string) => void;
  /** A message as it is, to the agent beside (or a new chat): «Пройти ревью». */
  send?: (text: string) => void;
  /** The chat beside the review, whose answers are read back. */
  chat?: ConvId | null;
  /** The machine of the chat beside. */
  machine?: MachineId;
  /** Opens the change in the «Ревью кода» app on the whole screen; absent in the app itself. */
  expand?: () => void;
}

export const MrStage = ({ url, ask, send, chat, machine, expand }: IMrStageProps) => {
  const { view, retry } = useChangeView(url);
  const review = useMrReview(url, MR_IO);
  const quotes = useQuotes(chat ?? null);
  const tools = useMemo(() => {
    if (!chat || !machine) return undefined;
    const quoted: IQuoted = {
      has: (id) => quotes.some((q) => q.id === id),
      toggle: (id, text, note) =>
        quotes.some((q) => q.id === id) ? removeQuote(chat, id) : putQuote(chat, { id, text, note }),
    };
    return {
      analyze: (question: string, signal: AbortSignal) => sideAnswer(machine, chat, question, signal),
      quoted,
    };
  }, [chat, machine, quotes]);
  const connect = useCallback(() => toConnections(url), [url]);
  const mr = useMrContext(review, connect, ask, tools);
  const io = useMemo<IAgentReviewIo | null>(
    () => (send ? { read: chat ? () => chatAnswers(chat) : NO_ANSWERS, send } : null),
    [send, chat],
  );
  const agent = useAgentReview(url, io);
  const ready = view.k === 'ready' ? view.view : null;
  const source = useMemo(
    () => (ready && !ready.limited && ready.kind !== 'issue' ? changeViewSource(ready) : null),
    [ready],
  );
  if (view.k === 'loading') return <StageSkeleton />;
  if (view.k === 'failed')
    return <Note icon="wifi-off" title={t('next.reviewApp.notRead')} text={view.text} onRetry={retry} />;
  if (view.view.limited)
    return (
      <Note
        icon="key-round"
        title={t('next.reviewApp.notRead')}
        text={t(`next.reviewApp.limit.${view.view.limited}`)}
        action={<OpenLink url={view.view.url} provider={view.view.provider} />}
      />
    );
  const out = <OpenLink url={view.view.url} provider={view.view.provider} />;
  if (!source || source.files.length === 0)
    return (
      <Note icon="git-compare" title={t('next.reviewApp.noChanges')} text={view.view.title} action={out} />
    );
  return (
    <ReviewApp
      source={source}
      threads={view.view.comments}
      mr={mr}
      agent={agent}
      actions={out}
      expand={expand}
    />
  );
};

// ── the work of a chat ──

const StageSkeleton = () => (
  <div className="hx-rvapp__wait" aria-busy="true">
    <PageSkeleton />
  </div>
);

type TSet =
  { k: 'loading' } | { k: 'offline' } | { k: 'failed'; text: string } | { k: 'ready'; set: IChangeSet };

const LOADING: TSet = { k: 'loading' };

const entryKey = (e: IWorkEntry) => `${e.machineId ?? ''}\u0000${e.conv}\u0000${e.worktree}`;

export const WorkStage = ({ machine, chat, wt }: { machine: MachineId; chat: ConvId; wt: string | null }) => {
  const notes = useReviewNotes(machine, chat);
  const [created, setCreated] = useState<{ worktree: string; url: string } | null>(null);
  const [work, setWork] = useState<{ chat: string; work: IChatWork | null } | null>(null);
  useEffect(() => {
    let live = true;
    void fetchChatWork(chat).then((got) => {
      if (live) setWork({ chat, work: got });
    });
    return () => {
      live = false;
    };
  }, [chat]);
  const current = work?.chat === chat ? work.work : undefined;
  const entries = useMemo(
    () => (current ? groupWork(current.entries).flatMap((repo) => repo.worktrees) : []),
    [current],
  );
  const entry = entries.find((e) => e.worktree === wt) ?? entries[0] ?? null;
  const offline = entry?.machineId
    ? current?.machines.find((m) => m.id === entry.machineId)?.state === 'offline'
    : false;

  const [loaded, setLoaded] = useState<{ key: string; set: TSet } | null>(null);
  // A save or a commit reads the worktree again; the change on screen stays until the new one comes.
  const [reads, setReads] = useState(0);
  const key = entry ? entryKey(entry) : '';
  useEffect(() => {
    if (!entry) return;
    if (offline) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- the machine is known off: nothing to ask it
      setLoaded({ key, set: { k: 'offline' } });
      return;
    }
    let live = true;
    fetchMachineChanges(entry)
      .then((set) => {
        if (live) setLoaded({ key, set: set ? { k: 'ready', set } : { k: 'offline' } });
      })
      .catch(() => {
        if (live) setLoaded({ key, set: { k: 'offline' } });
      });
    return () => {
      live = false;
    };
  }, [entry, key, offline, reads]);
  const set: TSet = loaded?.key === key ? loaded.set : LOADING;

  const fromForge = () => {
    if (!entry) return;
    setLoaded({ key, set: { k: 'loading' } });
    fetchRemoteChanges(entry)
      .then((got) => setLoaded({ key, set: { k: 'ready', set: got } }))
      .catch((e: unknown) =>
        setLoaded({ key, set: { k: 'failed', text: e instanceof Error ? e.message : String(e) } }),
      );
  };

  const source: IReviewSource | null = useMemo(
    () => (set.k === 'ready' && entry ? changeSetSource(set.set, entry) : null),
    [set, entry],
  );

  const onMachine = set.k === 'ready' && set.set.source === 'machine';
  const worktree = entry?.worktree ?? null;
  const machineId = entry?.machineId;
  // The chat that registered the worktree: the machine checks the write against its work.
  const owner = entry?.conv ?? chat;
  const edit = useMemo<IReviewEdit | null>(() => {
    if (!onMachine || !worktree) return null;
    const writer = workWriter(owner, worktree, machineId);
    return {
      ...writer,
      hash: contentHash,
      reload: () => setReads((n) => n + 1),
      askCommit: () => {
        sendMessage(machine, {
          conversationId: chat,
          text: t('next.reviewApp.edit.askCommitMessage', { worktree }),
        });
        toast(t('next.reviewApp.notes.asked'));
      },
    };
  }, [onMachine, worktree, machineId, owner, machine, chat]);
  const io = useMemo<IAgentReviewIo>(
    () => ({
      read: () => chatAnswers(chat),
      send: (text) => sendMessage(machine, { conversationId: chat, text }),
    }),
    [machine, chat],
  );
  const agent = useAgentReview(`${chat}\u0000${worktree ?? ''}`, io);

  if (current === undefined) return <StageSkeleton />;
  if (!entry)
    return (
      <Note icon="git-branch" title={t('next.reviewApp.noWork')} text={t('next.reviewApp.noWorkText')} />
    );

  const options: IWorkOption[] = entries.map((e) => ({
    key: e.worktree,
    name: e.worktree.replace(/\/+$/, '').split('/').at(-1) ?? e.worktree,
    branch: e.branch ?? null,
    mr: e.mr ? mrLabel(e.mr) : null,
    delegated: e.conv !== chat,
  }));
  const pick =
    options.length > 1 ? (
      <WorkPick
        options={options}
        current={entry.worktree}
        onPick={(to) => goTo({ kind: ROUTE.review, chat, wt: to }, { replace: true })}
      />
    ) : null;

  if (set.k === 'loading') return <StageSkeleton />;
  if (set.k === 'offline' || set.k === 'failed')
    return (
      <Note
        icon="wifi-off"
        title={t('next.reviewApp.machineOff')}
        text={set.k === 'failed' ? set.text : t('next.reviewApp.machineOffText')}
        action={
          entry.remote && entry.branch ? (
            <Button size="sm" icon="git-branch" onClick={fromForge}>
              {t('next.reviewApp.fromForge')}
            </Button>
          ) : undefined
        }
      />
    );
  if (!source || source.files.length === 0)
    return (
      <Note icon="git-compare" title={t('next.reviewApp.noChanges')} text={entry.branch ?? entry.worktree} />
    );
  const mrUrl = entry.mr ?? (created?.worktree === entry.worktree ? created.url : null);
  const firstTitle = source.meta.kind === 'local' ? source.meta.commits[0]?.title : undefined;
  let own: ReactNode = null;
  if (mrUrl) own = <OwnMr key={mrUrl} url={mrUrl} />;
  else if (entry.remote && entry.branch)
    own = (
      <CreateMr
        key={entry.worktree}
        remote={entry.remote}
        branch={entry.branch}
        title={firstTitle ?? entry.branch}
        onCreated={(url) => setCreated({ worktree: entry.worktree, url })}
      />
    );
  return (
    <ReviewApp
      source={source}
      pick={pick}
      footer={own}
      notes={notes}
      worktree={entry.worktree}
      edit={edit}
      agent={agent}
      actions={
        mrUrl ? (
          <OpenLink url={mrUrl} provider={forgeOf(mrUrl) === FORGE.github ? 'github' : 'gitlab'} />
        ) : undefined
      }
    />
  );
};
