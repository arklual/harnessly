import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import dayjs from 'dayjs';
import 'dayjs/locale/ru';
import {
  ArrowLeft,
  Bot,
  Check,
  ExternalLink,
  FileText,
  MessageSquare,
  MessageSquarePlus,
  Pencil,
  RotateCcw,
  Search,
  Shapes,
  SlidersHorizontal,
  X,
} from 'lucide-react';
import { cx } from '@/components/agents/ui';
import { isPageHidden } from '@/lib/frame-visibility';
import {
  createDocThread,
  docContentSrc,
  fetchDocDetail,
  fetchDocDiff,
  fetchDocSource,
  fetchDocVersions,
  markDocRead,
  replyToDocThread,
  setDocThreadStatus,
} from '@/lib/doc-threads-client';
import { DOC_THREAD_STATUS } from '@/types/doc-threads';
import { MessageText, Pill } from '@/components/agents/docs/mention-field';
import { plainMentions } from '@/lib/doc-mentions';
import { DocShare } from '@/components/agents/docs/doc-share';
import { DocEditorCard, useDocEditor, type IEditInfo } from '@/components/agents/docs/doc-editor';
import { DiffPanel, VersionMenu, VersionNotice } from '@/components/agents/docs/doc-versions';
import type { TDiffState } from '@/components/agents/docs/doc-versions';
import { currentVersion, findVersion, orderedPair, previousVersion, versionId } from '@/lib/doc-versions';
import { isInView, placeCommentButton, placeThread } from '@/lib/doc-placement';
import type { IBox } from '@/lib/doc-placement';
import type {
  IDocAnchor,
  IDocDetailResponse,
  IDocRect,
  IDocThread,
  IDocVersion,
  TDocFrameMessage,
  TDocParentMessage,
} from '@/types/doc-threads';
import { locale, plural, t } from '@/lib/i18n';

const POLL_MS = 10_000;

type TFilter = 'open' | 'resolved' | 'all';
const FILTER_CYCLE: readonly TFilter[] = [DOC_THREAD_STATUS.open, DOC_THREAD_STATUS.resolved, 'all'];
const FILTER_LABEL: Record<TFilter, string> = {
  open: t('docs.docViewer.filterOpen'),
  resolved: t('docs.docViewer.filterResolved'),
  all: t('docs.docViewer.filterAll'),
};

const PIN_HUES = [262, 199, 162, 32, 350, 210, 88] as const;

/** A stable hue per author, so a person's pins and avatars match everywhere. */
const personColor = (seed: string): string => {
  let hash = 0;
  for (let i = 0; i < seed.length; i++) hash = (hash * 31 + seed.charCodeAt(i)) | 0;
  return `hsl(${PIN_HUES[Math.abs(hash) % PIN_HUES.length]} 62% 52%)`;
};

const colorVar = (color: string): CSSProperties => ({ ['--td-person' as string]: color });

const initial = (name: string): string => (name.trim()[0] || '?').toUpperCase();

const ago = (iso: string): string => dayjs(iso).locale(locale()).fromNow();

const replies = (n: number): string => plural(n, 'docs.docViewer.replies');

type TComment = IDocThread['comments'][number];

const deliveryLabel = (
  comment: TComment,
): { state: 'delivered' | 'pending' | 'error'; label: string } | null => {
  if (comment.author.kind !== 'person') return null;
  if (comment.deliveryError) return { state: 'error', label: comment.deliveryError };
  if (comment.deliveredAt) return { state: 'delivered', label: t('docs.docViewer.delivered') };
  return { state: 'pending', label: t('docs.docViewer.pending') };
};

/** How an author is named in a thread: an agent by its owner, a guest of a link as a guest. */
const authorName = (author: TComment['author']): string => {
  if (author.kind === 'agent') return t('docs.docViewer.agentAuthor', { name: author.name });
  if (author.kind === 'guest') return t('docs.docViewer.guestAuthor', { name: author.name });
  return author.name;
};

const Avatar = ({ comment }: { comment: TComment }) =>
  comment.author.kind === 'agent' ? (
    <span className="td-avatar is-agent" aria-hidden="true">
      <Bot />
    </span>
  ) : (
    <span
      className="td-avatar"
      style={colorVar(personColor(comment.author.email || comment.author.name))}
      aria-hidden="true"
    >
      {initial(comment.author.name)}
    </span>
  );

const Message = ({ comment, withDelivery }: { comment: TComment; withDelivery: boolean }) => {
  const delivery = withDelivery ? deliveryLabel(comment) : null;
  return (
    <div className="td-message">
      <Avatar comment={comment} />
      <div className="td-message-body">
        <div className="td-message-head">
          <span className="td-message-name">{authorName(comment.author)}</span>
          <span className="td-message-time">{ago(comment.createdAt)}</span>
        </div>
        <MessageText className="td-message-text" body={comment.body} />
        {delivery && (
          <span className="td-message-delivery" data-state={delivery.state}>
            {delivery.label}
          </span>
        )}
      </div>
    </div>
  );
};

interface IThreadProps {
  thread: IDocThread;
  number: number;
  /** Why this thread cannot be answered here (a board, an old version); null when it can. */
  lockedNote: string | null;
  /** False for a document no conversation is behind: the delivery marks would promise an agent that is not there. */
  delivers: boolean;
  onClose?: () => void;
  onReply: (threadId: string, body: string) => Promise<void>;
  onSetStatus: (threadId: string, status: IDocThread['status']) => Promise<void>;
}

const Thread = ({ thread, number, lockedNote, delivers, onClose, onReply, onSetStatus }: IThreadProps) => {
  const resolved = thread.status === DOC_THREAD_STATUS.resolved;
  return (
    <div className="td-thread">
      <div className="td-thread-head">
        <span className="td-thread-number">#{number}</span>
        {!lockedNote && (
          <button
            type="button"
            className="td-icon"
            data-on={resolved || undefined}
            title={resolved ? t('docs.docViewer.reopen') : t('docs.docViewer.resolve')}
            onClick={() =>
              void onSetStatus(thread.id, resolved ? DOC_THREAD_STATUS.open : DOC_THREAD_STATUS.resolved)
            }
          >
            {resolved ? <RotateCcw /> : <Check />}
          </button>
        )}
        {onClose && (
          <button type="button" className="td-icon" title={t('docs.docViewer.close')} onClick={onClose}>
            <X />
          </button>
        )}
      </div>
      <blockquote className="td-thread-quote">{thread.anchor.quote}</blockquote>
      <div className="td-thread-messages">
        {thread.comments.map((comment) => (
          <Message key={comment.id} comment={comment} withDelivery={!lockedNote && delivers} />
        ))}
      </div>
      {lockedNote ? (
        <p className="td-thread-note">{lockedNote}</p>
      ) : (
        <Pill
          placeholder={t('docs.docViewer.reply')}
          label={t('docs.docViewer.replyInThread')}
          onSubmit={(body) => onReply(thread.id, body)}
        />
      )}
    </div>
  );
};

const placeOf = (thread: IDocThread, lost: boolean, isBoard: boolean): string => {
  if (isBoard && !lost)
    return thread.anchor.elementId
      ? t('docs.docViewer.atElement', { quote: thread.anchor.quote })
      : t('docs.docViewer.onCanvas');
  return thread.anchor.headingText || t('docs.docViewer.documentPlace');
};

const BOARD_NOTE = t('docs.docViewer.boardNote');
/** Keep in step with hl-docs.css: below this width the threads' rail is a sheet over the page. */
const SHEET_RAIL = '(max-width: 767px)';
const OLD_VERSION_NOTE = t('docs.docViewer.oldVersionNote');
const VERSIONS_POLL_MS = 60_000;
/** Keep in step with the exit animation of `.td-card` in hl-docs.css. */
const CARD_EXIT_MS = 120;
const RAIL_STORAGE_KEY = 'hl-docs-rail';

/** The person's last choice for the comments panel; closed until they open it, and fine without storage. */
const readRailChoice = (): boolean => {
  try {
    return window.localStorage.getItem(RAIL_STORAGE_KEY) === '1';
  } catch {
    return false;
  }
};

const writeRailChoice = (open: boolean) => {
  try {
    window.localStorage.setItem(RAIL_STORAGE_KEY, open ? '1' : '0');
  } catch {
    // Private mode or blocked storage: the choice just lasts until the page is closed.
  }
};

const coords = (point: { top: number; left: number }): CSSProperties => ({
  ['--td-top' as string]: `${point.top}px`,
  ['--td-left' as string]: `${point.left}px`,
});

interface ISelection {
  anchor: IDocAnchor;
  rect: IDocRect;
  end: IDocRect;
}

export interface IDocViewerProps {
  slug: string;
  /** The thread to open and scroll to once the document has placed it. */
  focusThreadId?: string | null;
  /** The version on screen (its id, a commit hash in a git-backed portal); null is the current one. Its owner keeps it: the page in the address, the panel in the tab. */
  versionSha: string | null;
  onVersionChange: (sha: string | null) => void;
  /** The document told its title; the page names the browser tab with it, the panel its own tab. */
  onTitle?: (title: string) => void;
  /** «To conversations» of the page; a document inside the right panel has nowhere to go back to. */
  onBack?: () => void;
  /** Inside the right panel: the panel's tab names the document, so the bar is one slim row and the comments open over the document instead of beside it. */
  embedded?: boolean;
  /** False while the viewer is behind another tab of the panel: it keeps what it has and asks for nothing. */
  visible?: boolean;
}

/**
 * A published document or a board with its comment threads and versions. The page `/docs/<slug>` and a document tab
 * of the right panel are the same viewer; only who keeps the version and what surrounds it differs.
 */
const DocViewer = ({
  slug,
  focusThreadId = null,
  versionSha,
  onVersionChange,
  onTitle,
  onBack,
  embedded = false,
  visible: onScreen = true,
}: IDocViewerProps) => {
  const focusParam = focusThreadId;

  const [detail, setDetail] = useState<IDocDetailResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<TFilter>(DOC_THREAD_STATUS.open);
  const [search, setSearch] = useState('');
  const [railOpen, setRailOpen] = useState(false);
  const [positions, setPositions] = useState<Record<string, IDocRect | null>>({});
  const [selection, setSelection] = useState<ISelection | null>(null);
  const [draft, setDraft] = useState<ISelection | null>(null);
  const [openThreadId, setOpenThreadId] = useState<string | null>(focusParam);
  const [box, setBox] = useState<IBox>({ width: 0, height: 0 });
  const [columnRight, setColumnRight] = useState(0);
  const [leaving, setLeaving] = useState(false);
  const [versions, setVersions] = useState<IDocVersion[] | null>(null);
  const [versionsError, setVersionsError] = useState<string | null>(null);
  const [diff, setDiff] = useState<{ from: IDocVersion; to: IDocVersion } | null>(null);
  const [diffState, setDiffState] = useState<TDiffState>({ status: 'loading' });
  const diffTokenRef = useRef(0);
  /** How the person may edit the current version; null until the gateway has said, or when they may not. */
  const [editInfo, setEditInfo] = useState<IEditInfo | null>(null);
  const editable = editInfo !== null;
  /** Bumped by a saved edit: the page and the versions are read again. */
  const [round, setRound] = useState(0);
  const editableRef = useRef(false);
  const lockedRef = useRef(false);
  const leavingRef = useRef(false);

  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const frameReadyRef = useRef(false);
  const focusedOnceRef = useRef(false);
  const selectionRef = useRef<ISelection | null>(null);
  const draftRef = useRef<ISelection | null>(null);

  const versionsLoadedRef = useRef(false);
  const onScreenRef = useRef(onScreen);
  useEffect(() => {
    onScreenRef.current = onScreen;
  }, [onScreen]);
  const title = detail?.doc.title ?? null;
  useEffect(() => {
    if (title) onTitle?.(title);
  }, [title, onTitle]);

  const isBoard = detail?.doc.kind === 'board';
  const hasVersions = detail?.doc.kind === 'artifact';

  // `?v=<version id>` keeps the version in the address; without it the current one is shown.
  const viewedSha = versionSha;
  const viewed = findVersion(versions ?? [], viewedSha);
  const current = currentVersion(versions ?? []);
  const shown = viewed ?? current;
  const isOld = Boolean(viewed && !viewed.current);
  const versionMissing = Boolean(viewedSha && versions && !viewed);
  // A version that is not in the history is not asked for: the current page is shown with a note instead of the gateway's 404.
  const contentVersion = viewed ? versionId(viewed) : versions ? null : viewedSha;
  const lockedNote = (isBoard && BOARD_NOTE) || (isOld && OLD_VERSION_NOTE) || null;

  useEffect(() => {
    selectionRef.current = selection;
    draftRef.current = draft;
    lockedRef.current = isOld;
    editableRef.current = editable;
  }, [selection, draft, isOld, editable]);

  const refresh = useCallback(() => setRound((n) => n + 1), []);
  // On a phone the threads and an open thread are sheets over the page's bottom, where the edit's card sits: an edit makes room for itself.
  const roomForEditor = useCallback(() => {
    if (!window.matchMedia(SHEET_RAIL).matches) return;
    setRailOpen(false);
    setOpenThreadId(null);
  }, []);
  const toFrame = useCallback((message: TDocParentMessage) => {
    iframeRef.current?.contentWindow?.postMessage(message, '*');
  }, []);
  const editor = useDocEditor(slug, editInfo, toFrame, refresh, refresh);
  const { openAll: openEditor, openBlock, commit: commitBlock, dropped: editDropped } = editor;

  // Who may edit is asked once per version on screen: a new version may have no Markdown, a changed role no rights.
  const currentN = current?.n ?? null;
  useEffect(() => {
    if (!slug || !hasVersions) return undefined;
    let cancelled = false;
    fetchDocSource(slug).then(
      (source) =>
        !cancelled &&
        setEditInfo(source.canEdit && source.mode ? { mode: source.mode, version: source.version } : null),
      () => !cancelled && setEditInfo(null),
    );
    return () => {
      cancelled = true;
    };
  }, [slug, hasVersions, currentN]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- localStorage is invisible to the prerender; applied after hydration
    setRailOpen(readRailChoice());
  }, []);
  const chooseRail = useCallback((open: boolean) => {
    setRailOpen(open);
    writeRailChoice(open);
  }, []);

  const reload = useCallback(async () => {
    if (!slug) return;
    try {
      const next = await fetchDocDetail(slug);
      setDetail(next);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('docs.docViewer.errorLoad'));
    }
  }, [slug]);

  useEffect(() => {
    if (!slug) return undefined;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const tick = async () => {
      if (!isPageHidden() && onScreenRef.current) await reload();
      if (!cancelled) timer = setTimeout(tick, POLL_MS);
    };
    timer = setTimeout(async () => {
      await reload();
      if (!cancelled) timer = setTimeout(tick, POLL_MS);
    }, 0);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [slug, reload]);

  const lastActivity = detail?.doc.lastActivityAt;
  useEffect(() => {
    if (slug && lastActivity) void markDocRead(slug);
  }, [slug, lastActivity]);

  useEffect(() => {
    if (!slug || !hasVersions) return undefined;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const load = async () => {
      if (!isPageHidden() && (onScreenRef.current || versionsLoadedRef.current === false)) {
        try {
          const next = await fetchDocVersions(slug);
          versionsLoadedRef.current = true;
          if (cancelled) return;
          setVersions(next.versions);
          setVersionsError(null);
        } catch (err) {
          if (cancelled) return;
          setVersionsError(err instanceof Error ? err.message : t('docs.docViewer.errorVersions'));
        }
      }
      if (!cancelled) timer = setTimeout(load, VERSIONS_POLL_MS);
    };
    timer = setTimeout(load, 0);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [slug, hasVersions, round]);

  const postToFrame = useCallback((message: TDocParentMessage) => {
    iframeRef.current?.contentWindow?.postMessage(message, '*');
  }, []);

  const sendThreads = useCallback(() => {
    if (detail && frameReadyRef.current && !isBoard) {
      postToFrame({
        type: 'doc:threads',
        threads: detail.threads.map((t) => ({ id: t.id, anchor: t.anchor, status: t.status })),
      });
    }
  }, [detail, isBoard, postToFrame]);

  useEffect(sendThreads, [sendThreads]);

  const loaded = detail !== null;
  // The overlay is as big as the document frame: every rect from the frame is in these coordinates.
  useEffect(() => {
    const overlay = overlayRef.current;
    if (!overlay) return undefined;
    const measure = () => setBox({ width: overlay.clientWidth, height: overlay.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(overlay);
    return () => observer.disconnect();
  }, [isBoard, loaded]);

  const startDraft = useCallback(() => {
    const current = selectionRef.current;
    if (!current || leavingRef.current) return;
    setDraft(current);
    setSelection(null);
    postToFrame({ type: 'doc:draft', anchor: current.anchor });
  }, [postToFrame]);

  const cancelDraft = useCallback(() => {
    if (!draftRef.current || leavingRef.current) return;
    postToFrame({ type: 'doc:draft-clear' });
    // The card slides away first (its exit animation), then it is unmounted.
    leavingRef.current = true;
    setLeaving(true);
    window.setTimeout(() => {
      leavingRef.current = false;
      setLeaving(false);
      setDraft(null);
    }, CARD_EXIT_MS);
  }, [postToFrame]);

  useEffect(() => {
    const onMessage = (event: MessageEvent<TDocFrameMessage>) => {
      if (event.source !== iframeRef.current?.contentWindow) return;
      const data = event.data;
      if (!data || typeof data.type !== 'string') return;
      if (data.type === 'doc:selection') {
        if (!lockedRef.current) setSelection({ anchor: data.anchor, rect: data.rect, end: data.end });
      } else if (data.type === 'doc:selection-cleared') setSelection(null);
      else if (data.type === 'doc:dismiss') cancelDraft();
      else if (data.type === 'doc:comment-key') startDraft();
      else if (data.type === 'doc:edit-block') {
        if (!editableRef.current || lockedRef.current) return;
        setSelection(null);
        roomForEditor();
        openBlock({
          tag: data.tag,
          text: data.text,
          ordinal: data.ordinal,
          total: data.total,
          index: data.index,
        });
      } else if (data.type === 'doc:edit-commit') commitBlock(data);
      else if (data.type === 'doc:edit-cancel') editDropped();
      else if (data.type === 'doc:open') {
        setOpenThreadId(data.threadId);
        setSelection(null);
      } else if (data.type === 'doc:anchors') {
        setPositions(data.positions);
        setColumnRight(data.columnRight);
        if (!focusedOnceRef.current && focusParam && data.positions[focusParam]) {
          focusedOnceRef.current = true;
          postToFrame({ type: 'doc:focus', threadId: focusParam });
        }
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [focusParam, postToFrame, cancelDraft, startDraft, openBlock, commitBlock, editDropped, roomForEditor]);

  // Keys and presses outside the frame: the same shortcut and the same «click away cancels».
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.altKey && (event.code === 'KeyM' || event.key === 'm')) {
        event.preventDefault();
        startDraft();
      }
    };
    const onPress = (event: PointerEvent) => {
      if (draftRef.current && !(event.target as Element | null)?.closest?.('.td-composer')) cancelDraft();
    };
    window.addEventListener('keydown', onKey);
    window.addEventListener('pointerdown', onPress);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('pointerdown', onPress);
    };
  }, [startDraft, cancelDraft]);

  const pickVersion = (version: IDocVersion | null) => {
    onVersionChange(version && !version.current ? versionId(version) : null);
  };

  const openDiff = (a: IDocVersion, b: IDocVersion) => {
    const pair = orderedPair(a, b);
    const token = diffTokenRef.current + 1;
    diffTokenRef.current = token;
    setDiff(pair);
    setDiffState({ status: 'loading' });
    if (!slug) return;
    fetchDocDiff(slug, versionId(pair.from), versionId(pair.to)).then(
      (result) => token === diffTokenRef.current && setDiffState({ status: 'ok', diff: result }),
      (err: unknown) =>
        token === diffTokenRef.current &&
        setDiffState({
          status: 'error',
          message: err instanceof Error ? err.message : t('docs.docViewer.errorCompare'),
        }),
    );
  };

  const onReply = async (threadId: string, body: string) => {
    if (!slug) return;
    await replyToDocThread(slug, threadId, body);
    await reload();
  };

  const onSetStatus = async (threadId: string, status: IDocThread['status']) => {
    if (!slug) return;
    await setDocThreadStatus(slug, threadId, status);
    await reload();
  };

  const onCreate = async (body: string) => {
    if (!slug || !draft) return;
    try {
      const created = await createDocThread(slug, draft.anchor, body);
      cancelDraft();
      await reload();
      // The new thread opens in the popover by its pin; the comments panel stays as the person left it.
      if (created.thread?.id) setOpenThreadId(created.thread.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('docs.docViewer.errorSend'));
    }
  };

  /** Numbers follow creation order over every thread, so «#3» means the same thread whatever the filter. */
  const numbers = useMemo(() => new Map((detail?.threads ?? []).map((t, i) => [t.id, i + 1])), [detail]);

  const visible = useMemo(() => {
    if (!detail) return null;
    const q = search.trim().toLowerCase();
    return detail.threads
      .filter((t) => filter === 'all' || t.status === filter)
      .filter(
        (t) =>
          !q ||
          t.anchor.quote.toLowerCase().includes(q) ||
          t.comments.some((c) => c.body.toLowerCase().includes(q) || c.author.name.toLowerCase().includes(q)),
      );
  }, [detail, filter, search]);

  const isLost = (thread: IDocThread) => !isBoard && positions[thread.id] === null;
  const placed = (visible ?? []).filter((t) => !isLost(t));
  const lost = (visible ?? []).filter(isLost);
  const openCount = detail ? detail.threads.filter((t) => t.status === DOC_THREAD_STATUS.open).length : 0;
  const openThread = openThreadId ? (detail?.threads.find((t) => t.id === openThreadId) ?? null) : null;
  const openRect = openThread && !isBoard ? (positions[openThread.id] ?? null) : null;
  const placement = openThread && openRect ? placeThread(openRect, box, columnRight) : null;
  const popover = placement?.mode === 'gutter' && isInView(openRect, box) ? placement : null;
  const threadCard = placement?.mode === 'card';

  const cycleFilter = () =>
    setFilter((current) => FILTER_CYCLE[(FILTER_CYCLE.indexOf(current) + 1) % FILTER_CYCLE.length]);
  const focusThread = (thread: IDocThread) => {
    setOpenThreadId((current) => (current === thread.id ? null : thread.id));
    if (!isBoard && positions[thread.id]) postToFrame({ type: 'doc:focus', threadId: thread.id });
  };

  if (!slug) return null;

  return (
    <>
      <div className="td-app" data-embedded={embedded || undefined}>
        <header className="td-bar">
          {onBack && (
            <button
              type="button"
              className="td-icon td-back"
              title={t('docs.docViewer.back')}
              onClick={onBack}
            >
              <ArrowLeft />
            </button>
          )}
          <span className="td-bar-kind" aria-hidden="true">
            {isBoard ? <Shapes /> : <FileText />}
          </span>
          <div className="td-bar-title">
            <h1>{detail ? detail.doc.title : t('docs.docViewer.loading')}</h1>
            {detail && (
              <span className="td-bar-meta">
                {[detail.doc.owner?.name, detail.doc.machineId, detail.doc.sourcePath]
                  .filter(Boolean)
                  .join(' · ')}
              </span>
            )}
          </div>
          {hasVersions && (
            <VersionMenu
              versions={versions}
              error={versionsError}
              shown={shown}
              onPick={pickVersion}
              onCompare={(version) => shown && openDiff(version, shown)}
            />
          )}
          <div className="td-bar-actions">
            {hasVersions && editable && !isOld && (
              <button
                type="button"
                className="td-icon"
                data-on={editor.editing !== null || undefined}
                aria-pressed={editor.editing !== null}
                title={t('docs.docEditor.edit')}
                onClick={() => {
                  roomForEditor();
                  openEditor();
                }}
              >
                <Pencil />
              </button>
            )}
            {detail && isBoard && (
              <a
                className="td-icon"
                href={detail.doc.url}
                target="_blank"
                rel="noreferrer"
                title={t('docs.docViewer.openInCatalog')}
              >
                <ExternalLink />
              </a>
            )}
            {detail && <DocShare slug={detail.doc.slug} title={detail.doc.title} kind={detail.doc.kind} />}
            <button
              type="button"
              className="td-comments-toggle"
              data-on={(railOpen && !diff) || undefined}
              aria-pressed={railOpen && !diff}
              title={railOpen && !diff ? t('docs.docViewer.hideComments') : t('docs.docViewer.showComments')}
              onClick={() => {
                if (diff) {
                  setDiff(null);
                  chooseRail(true);
                } else chooseRail(!railOpen);
              }}
            >
              <MessageSquare />
              {openCount > 0 && <span>{openCount}</span>}
            </button>
          </div>
        </header>

        {error && (
          <p className="td-error" role="status">
            {error}
          </p>
        )}
        {isOld && viewed && (
          <VersionNotice
            version={viewed}
            hasPrevious={Boolean(versions && previousVersion(versions, viewed))}
            onBack={() => pickVersion(null)}
            onDiff={() => {
              const previous = versions ? previousVersion(versions, viewed) : null;
              if (previous) openDiff(previous, viewed);
            }}
          />
        )}
        {versionMissing && (
          <p className="td-error" role="status">
            {t('docs.docViewer.versionMissing', { sha: viewedSha ?? '' })}
          </p>
        )}
        {detail && !detail.doc.bound && (
          <p className="td-unbound" role="note">
            {t('docs.docViewer.unbound')}
          </p>
        )}

        <div className="td-body">
          <main className="td-canvas">
            {isBoard && detail ? (
              <iframe
                className="td-board"
                src={detail.doc.url}
                title={detail.doc.title}
                allow="clipboard-read; clipboard-write; fullscreen"
              />
            ) : (
              <>
                {/* No sandbox attribute: the runtime redirects to the catalog's read link, which sandboxes the document
                    itself; a sandboxed parent gives that page an opaque origin and its document frame is refused. */}
                <iframe
                  key={`${contentVersion ?? 'current'}:${round}`}
                  ref={iframeRef}
                  src={docContentSrc(slug, contentVersion)}
                  allow="fullscreen"
                  className="td-frame"
                  data-testid="document-frame"
                  title={detail ? detail.doc.title : t('docs.docViewer.document')}
                  onLoad={() => {
                    frameReadyRef.current = true;
                    setSelection(null);
                    setPositions({});
                    sendThreads();
                  }}
                />
                <div className="td-overlay" ref={overlayRef}>
                  {detail?.threads.map((thread) => {
                    const rect = positions[thread.id];
                    if (!isInView(rect, box) || (filter !== 'all' && thread.status !== filter)) return null;
                    const opener = thread.comments[0];
                    return (
                      <button
                        key={thread.id}
                        type="button"
                        className={cx(
                          'td-pin',
                          thread.status === DOC_THREAD_STATUS.resolved && 'is-resolved',
                          openThreadId === thread.id && 'is-active',
                        )}
                        style={{
                          ...coords({ top: rect.top, left: rect.left + rect.width }),
                          ...colorVar(personColor(opener?.author.email || opener?.author.name || thread.id)),
                        }}
                        onClick={() => focusThread(thread)}
                        aria-label={t('docs.docViewer.threadLabel', {
                          n: numbers.get(thread.id) ?? '',
                          quote: thread.anchor.quote,
                        })}
                      >
                        {opener ? initial(opener.author.name) : '?'}
                      </button>
                    );
                  })}

                  {openThread && popover && (
                    <div className="td-popover" style={coords(popover)}>
                      <Thread
                        thread={openThread}
                        number={numbers.get(openThread.id) ?? 0}
                        lockedNote={lockedNote}
                        delivers={detail?.doc.bound ?? false}
                        onClose={() => setOpenThreadId(null)}
                        onReply={onReply}
                        onSetStatus={onSetStatus}
                      />
                    </div>
                  )}

                  {openThread && threadCard && !draft && (
                    <div className="td-card">
                      <Thread
                        thread={openThread}
                        number={numbers.get(openThread.id) ?? 0}
                        lockedNote={lockedNote}
                        delivers={detail?.doc.bound ?? false}
                        onClose={() => setOpenThreadId(null)}
                        onReply={onReply}
                        onSetStatus={onSetStatus}
                      />
                    </div>
                  )}

                  {draft && (
                    <div
                      className={cx('td-card', 'td-composer', leaving && 'is-leaving')}
                      role="dialog"
                      aria-label={t('docs.docViewer.newComment')}
                    >
                      <div className="td-composer-head">
                        <span className="td-composer-quote" title={draft.anchor.quote}>
                          {draft.anchor.quote}
                        </span>
                        <button
                          type="button"
                          className="td-icon"
                          title={t('docs.docViewer.cancelEsc')}
                          onClick={cancelDraft}
                        >
                          <X />
                        </button>
                      </div>
                      <Pill
                        placeholder={t('docs.docViewer.addComment')}
                        label={t('docs.docViewer.newComment')}
                        autoFocus
                        onSubmit={onCreate}
                        onCancel={cancelDraft}
                      />
                    </div>
                  )}

                  <DocEditorCard editor={editor} />

                  {selection && !draft && !editor.editing && (
                    <button
                      type="button"
                      className="td-selbtn"
                      style={coords(placeCommentButton(selection, box))}
                      title={t('docs.docViewer.commentShortcut')}
                      onClick={startDraft}
                    >
                      <MessageSquarePlus aria-hidden="true" />
                      <span>{t('docs.docViewer.comment')}</span>
                    </button>
                  )}
                </div>
              </>
            )}
          </main>

          {diff && (
            <DiffPanel
              from={diff.from}
              to={diff.to}
              state={diffState}
              shown={shown}
              onFind={(text) => postToFrame({ type: 'doc:find', text })}
              onClose={() => setDiff(null)}
            />
          )}
          {!diff && railOpen && (
            <aside className="td-rail" aria-label={t('docs.docViewer.comments')}>
              <div className="td-rail-head">
                <label className="td-search">
                  <Search aria-hidden="true" />
                  <input
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder={t('docs.docViewer.search')}
                    aria-label={t('docs.docViewer.searchComments')}
                  />
                </label>
                <button
                  type="button"
                  className="td-icon"
                  data-on={filter !== DOC_THREAD_STATUS.open || undefined}
                  title={t('docs.docViewer.shownFilter', { filter: FILTER_LABEL[filter].toLowerCase() })}
                  onClick={cycleFilter}
                >
                  <SlidersHorizontal />
                </button>
                <button
                  type="button"
                  className="td-icon"
                  title={t('docs.docViewer.hideComments')}
                  onClick={() => chooseRail(false)}
                >
                  <X />
                </button>
              </div>
              {filter !== DOC_THREAD_STATUS.open && <p className="td-rail-filter">{FILTER_LABEL[filter]}</p>}

              <div className="td-rail-list">
                {visible === null && <p className="td-rail-empty">{t('docs.docViewer.loadingThreads')}</p>}
                {visible?.length === 0 && (
                  <div className="td-rail-empty">
                    <MessageSquare aria-hidden="true" />
                    <span>
                      {search
                        ? t('docs.docViewer.nothingFound')
                        : isBoard
                          ? t('docs.docViewer.emptyBoard')
                          : t('docs.docViewer.emptyDocument')}
                    </span>
                  </div>
                )}
                {[...placed, ...lost].map((thread, i) => {
                  const opener = thread.comments[0];
                  const threadLost = isLost(thread);
                  const expanded = openThreadId === thread.id && (isBoard || threadLost || !openRect);
                  return (
                    <div
                      key={thread.id}
                      className={cx('td-row-wrap', i === placed.length && lost.length > 0 && 'starts-lost')}
                      data-lost-label={t('docs.docViewer.lostPlace')}
                    >
                      <button
                        type="button"
                        className={cx(
                          'td-row',
                          openThreadId === thread.id && 'is-active',
                          thread.status === DOC_THREAD_STATUS.resolved && 'is-resolved',
                        )}
                        onClick={() => focusThread(thread)}
                      >
                        {opener && <Avatar comment={opener} />}
                        <span className="td-row-body">
                          <span className="td-row-meta">
                            #{numbers.get(thread.id)} · {placeOf(thread, threadLost, isBoard)}
                          </span>
                          {opener && (
                            <span className="td-row-by">
                              <b>
                                {opener.author.kind === 'agent'
                                  ? t('docs.docViewer.agent')
                                  : authorName(opener.author)}
                              </b>{' '}
                              ·{' '}
                              {ago(
                                thread.comments[thread.comments.length - 1]?.createdAt ?? opener.createdAt,
                              )}
                            </span>
                          )}
                          {opener && <span className="td-row-text">{plainMentions(opener.body)}</span>}
                          {thread.comments.length > 1 && (
                            <span className="td-row-count">{replies(thread.comments.length - 1)}</span>
                          )}
                        </span>
                      </button>
                      {expanded && (
                        <div className="td-row-thread">
                          <Thread
                            thread={thread}
                            number={numbers.get(thread.id) ?? 0}
                            lockedNote={lockedNote}
                            delivers={detail?.doc.bound ?? false}
                            onReply={onReply}
                            onSetStatus={onSetStatus}
                          />
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </aside>
          )}
        </div>
      </div>
    </>
  );
};

export default DocViewer;
