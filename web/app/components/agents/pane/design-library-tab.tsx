import { useMemo, useRef, useState, type DragEvent, type KeyboardEvent, type ReactNode } from 'react';
import dayjs from 'dayjs';
import {
  ChevronRight,
  Eye,
  FolderOpen,
  LayoutTemplate,
  MessageSquare,
  MoreHorizontal,
  Share2,
  SquareArrowOutUpRight,
  Upload,
} from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CONFIRM_TONE,
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwButton,
  TwConfirm,
  TwMenuButton,
  TwSpinner,
  TwTooltip,
  cx,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import { DesignCheckPanel } from '@/components/agents/pane/design-check-panel';
import useDesignLink, { openFullLabel } from '@/hooks/use-design-link';
import useSendBoardToPhone from '@/hooks/use-send-board-to-phone';
import type { TDesignLibrary } from '@/hooks/use-design-library';
import { currentMachineId, scopedAppPath } from '@/lib/app-path';
import { openConversation } from '@/lib/hl-open-conversation';
import {
  continueDesignDialog,
  editDesignBoards,
  messageDesignAgent,
  uploadDesignProject,
} from '@/lib/hl-design-library-client';
import { AGENT_STATE_LABEL, groupArtifacts, isAgentRunning, mainBoard } from '@/lib/hl-design-library/group';
import { claimsLine } from '@/lib/hl-design-library/home-model';
import { boardsLabel } from '@/lib/hl-design-library/home-model';
import {
  libraryArtifactAddress,
  libraryBoardAddress,
  type IArtifactAgent,
  type ILibraryArtifact,
  type ILibraryBoard,
} from '@/lib/hl-design-library/types';
import { t } from '@/lib/i18n';

/** A list this long gets a filter: the admin project alone has 57 boards. */
const FILTER_FROM = 12;
const COPIED_MS = 1_500;

type TOpenBoard = (address: string, title: string) => void;

const when = (ms: number): string => (ms > 0 ? dayjs(ms).format('DD.MM HH:mm') : '');

interface IBoardListProps {
  artifact: ILibraryArtifact;
  port: number;
  onOpenBoard: TOpenBoard;
  onChanged: () => void;
}

const BoardList = ({ artifact, port, onOpenBoard, onChanged }: IBoardListProps) => {
  const [query, setQuery] = useState('');
  const [renaming, setRenaming] = useState<string | null>(null);
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [removing, setRemoving] = useState<ILibraryBoard | null>(null);
  const { share, openFull } = useDesignLink();
  const shown = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return needle
      ? artifact.boards.filter((board) => board.title.toLowerCase().includes(needle))
      : artifact.boards;
  }, [artifact.boards, query]);

  const edit = async (change: Parameters<typeof editDesignBoards>[2]) => {
    setBusy(true);
    setError(null);
    try {
      await editDesignBoards(currentMachineId(), artifact.id, change);
      onChanged();
      return true;
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('pane.designLibraryTab.editFailed'));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const saveName = async (board: ILibraryBoard) => {
    if (!draft.trim() || draft.trim() === board.title) return setRenaming(null);
    if (await edit({ action: 'rename', path: board.path, title: draft })) setRenaming(null);
  };

  return (
    <div className="tw-dlib-boards">
      {artifact.boards.length >= FILTER_FROM && (
        <input
          type="search"
          className="tw-dlib-filter"
          value={query}
          placeholder={t('pane.designLibraryTab.findBoard')}
          aria-label={t('pane.designLibraryTab.findBoardIn', { title: artifact.title })}
          autoComplete="off"
          onChange={(event) => setQuery(event.target.value)}
        />
      )}
      {error && (
        <p className="tw-dlib-note tw-dlib-note-error" role="alert">
          {error}
        </p>
      )}
      {shown.length === 0 ? (
        <p className="tw-rpane-muted">
          {artifact.boards.length === 0
            ? t('pane.designLibraryTab.noBoards')
            : t('pane.designLibraryTab.boardNotFound')}
        </p>
      ) : (
        <ul className="tw-dlib-board-list">
          {shown.map((board) => {
            const at = artifact.boards.indexOf(board);
            const file = board.path.endsWith('.dc.html') && !board.path.includes('/');
            return (
              <li key={board.path} className="tw-dlib-board-row">
                {renaming === board.path ? (
                  <input
                    className="tw-dlib-filter"
                    value={draft}
                    autoFocus
                    aria-label={t('pane.designLibraryTab.newBoardName')}
                    maxLength={120}
                    disabled={busy}
                    onChange={(event) => setDraft(event.target.value)}
                    onBlur={() => void saveName(board)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') void saveName(board);
                      if (event.key === 'Escape') setRenaming(null);
                    }}
                  />
                ) : (
                  <button
                    type="button"
                    className="tw-dlib-board"
                    title={board.title}
                    onClick={() =>
                      onOpenBoard(libraryBoardAddress(port, artifact.id, board.path), board.title)
                    }
                  >
                    {board.title}
                  </button>
                )}
                {renaming !== board.path && (
                  <TwMenuButton
                    items={
                      [
                        {
                          kind: TW_MENU_KIND.action,
                          id: 'share',
                          label: t('pane.designLibraryTab.shareLink'),
                          onSelect: () =>
                            void share({ project: artifact.id, board: board.path, full: false }, board.title),
                        },
                        {
                          kind: TW_MENU_KIND.action,
                          id: 'full',
                          label: openFullLabel(),
                          onSelect: () => openFull({ project: artifact.id, board: board.path, full: true }),
                        },
                        { kind: TW_MENU_KIND.separator, id: 'sep-link' },
                        {
                          kind: TW_MENU_KIND.action,
                          id: 'rename',
                          label: t('pane.designLibraryTab.rename'),
                          onSelect: () => {
                            setDraft(board.title);
                            setRenaming(board.path);
                          },
                        },
                        ...(file
                          ? [
                              {
                                kind: TW_MENU_KIND.action,
                                id: 'copy',
                                label: t('pane.designLibraryTab.duplicate'),
                                onSelect: () => void edit({ action: 'duplicate', path: board.path }),
                              },
                            ]
                          : []),
                        ...(at > 0
                          ? [
                              {
                                kind: TW_MENU_KIND.action,
                                id: 'up',
                                label: t('pane.designLibraryTab.moveUp'),
                                onSelect: () => void edit({ action: 'move', path: board.path, delta: -1 }),
                              },
                            ]
                          : []),
                        ...(at < artifact.boards.length - 1
                          ? [
                              {
                                kind: TW_MENU_KIND.action,
                                id: 'down',
                                label: t('pane.designLibraryTab.moveDown'),
                                onSelect: () => void edit({ action: 'move', path: board.path, delta: 1 }),
                              },
                            ]
                          : []),
                        { kind: TW_MENU_KIND.separator, id: 'sep' },
                        {
                          kind: TW_MENU_KIND.action,
                          id: 'remove',
                          label: t('pane.designLibraryTab.delete'),
                          danger: true,
                          onSelect: () => setRemoving(board),
                        },
                      ] as TTwMenuEntry[]
                    }
                    menuLabel={t('pane.designLibraryTab.boardMenu', { title: board.title })}
                    align={LAYER_ALIGN.end}
                    className="tw-icon-button tw-dlib-board-more"
                    aria-label={t('pane.designLibraryTab.boardActions', { title: board.title })}
                    title={t('pane.designLibraryTab.actions')}
                  >
                    <MoreHorizontal />
                  </TwMenuButton>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <TwConfirm
        open={removing !== null}
        title={t('pane.designLibraryTab.deleteBoardTitle', { title: removing?.title ?? '' })}
        description={t('pane.designLibraryTab.deleteBoardDescription')}
        confirmLabel={t('pane.designLibraryTab.delete')}
        tone={CONFIRM_TONE.danger}
        pending={busy}
        error={error}
        onConfirm={() =>
          void (
            removing &&
            edit({ action: 'remove', path: removing.path }).then((ok) => {
              if (ok) setRemoving(null);
            })
          )
        }
        onCancel={() => setRemoving(null)}
      />
    </div>
  );
};

interface IAgentRowProps {
  agent: IArtifactAgent;
  artifact: ILibraryArtifact;
}

/** One conversation working on the design: its state, and for another agent — open it or write to it from here. */
const AgentRow = ({ agent, artifact }: IAgentRowProps) => {
  const [composing, setComposing] = useState(false);
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null);

  const send = async () => {
    if (!text.trim() || sending) return;
    setSending(true);
    setResult(null);
    try {
      await messageDesignAgent(currentMachineId(), agent.tabId, artifact.id, text);
      setText('');
      setComposing(false);
      setResult({ ok: true, message: t('pane.designLibraryTab.sentToAgent') });
    } catch (error) {
      setResult({
        ok: false,
        message: error instanceof Error ? error.message : t('pane.designLibraryTab.sendFailed'),
      });
    } finally {
      setSending(false);
    }
  };
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void send();
    } else if (event.key === 'Escape') {
      setComposing(false);
    }
  };

  return (
    <li className="tw-dlib-agent" data-self={agent.self || undefined}>
      <div className="tw-dlib-agent-line">
        <span className="tw-dlib-agent-dot" data-state={agent.state} aria-hidden="true" />
        <span className="tw-dlib-agent-name" title={agent.title}>
          {agent.self ? t('pane.designLibraryTab.thisAgent') : agent.title}
        </span>
        <span className="tw-dlib-agent-state">
          {AGENT_STATE_LABEL[agent.state]}
          {!agent.self && agent.workspace ? ` · ${agent.workspace}` : ''}
        </span>
        {!agent.self && (
          <span className="tw-dlib-agent-actions">
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              icon={<SquareArrowOutUpRight />}
              onClick={() => openConversation(agent.tabId)}
            >
              {t('pane.designLibraryTab.openConversation')}
            </TwButton>
            {isAgentRunning(agent.state) && (
              <TwButton
                size={BUTTON_SIZE.sm}
                variant={BUTTON_VARIANT.ghost}
                icon={<MessageSquare />}
                aria-expanded={composing}
                onClick={() => setComposing((open) => !open)}
              >
                {t('pane.designLibraryTab.write')}
              </TwButton>
            )}
          </span>
        )}
      </div>
      {composing && (
        <div className="tw-dlib-compose">
          <input
            className="tw-dlib-filter"
            value={text}
            maxLength={2000}
            placeholder={t('pane.designLibraryTab.messagePlaceholder')}
            aria-label={t('pane.designLibraryTab.messageTo', { title: agent.title })}
            autoFocus
            onChange={(event) => setText(event.target.value)}
            onKeyDown={onKey}
          />
          <TwButton
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.primary}
            pending={sending}
            disabled={!text.trim()}
            onClick={() => void send()}
          >
            {t('pane.designLibraryTab.send')}
          </TwButton>
        </div>
      )}
      {result && (
        <p
          className={cx('tw-dlib-note', !result.ok && 'tw-dlib-note-error')}
          role={result.ok ? 'status' : 'alert'}
        >
          {result.message}
        </p>
      )}
    </li>
  );
};

interface IArtifactCardProps {
  artifact: ILibraryArtifact;
  port: number;
  onOpenBoard: TOpenBoard;
  onChanged: () => void;
}

const initials = (title: string): string =>
  title
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0])
    .join('')
    .toUpperCase() || 'CD';

const ArtifactCard = ({ artifact, port, onOpenBoard, onChanged }: IArtifactCardProps) => {
  const { send: sendToPhone } = useSendBoardToPhone();
  const { share } = useDesignLink();
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const [starting, setStarting] = useState(false);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [draft, setDraft] = useState(artifact.title);
  const [checkNow, setCheckNow] = useState(false);
  const first = mainBoard(artifact);
  const others = artifact.agents.filter((agent) => !agent.self);
  const me = artifact.agents.find((agent) => agent.self);

  const startDialog = async () => {
    setStarting(true);
    setNote(null);
    try {
      const { tabId } = await continueDesignDialog(currentMachineId(), artifact.id);
      openConversation(tabId);
    } catch (error) {
      setNote({
        ok: false,
        text: error instanceof Error ? error.message : t('pane.designLibraryTab.dialogFailed'),
      });
    } finally {
      setStarting(false);
    }
  };
  const copyPath = async () => {
    try {
      await navigator.clipboard.writeText(artifact.projectPath);
    } catch {
      return;
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), COPIED_MS);
  };
  const saveTitle = async () => {
    setRenaming(false);
    if (!draft.trim() || draft.trim() === artifact.title) return;
    try {
      await editDesignBoards(currentMachineId(), artifact.id, { action: 'title', title: draft });
      onChanged();
    } catch (error) {
      setNote({
        ok: false,
        text: error instanceof Error ? error.message : t('pane.designLibraryTab.renameFailed'),
      });
    }
  };

  const menu: TTwMenuEntry[] = [
    {
      kind: TW_MENU_KIND.action,
      id: 'boards',
      label: expanded ? t('pane.designLibraryTab.hideBoards') : t('pane.designLibraryTab.showBoards'),
      onSelect: () => setExpanded((open) => !open),
    },
    {
      kind: TW_MENU_KIND.action,
      id: 'check',
      label: t('pane.designLibraryTab.checkBoards'),
      onSelect: () => {
        setCheckNow(true);
        setExpanded(true);
      },
    },
    {
      kind: TW_MENU_KIND.action,
      id: 'rename',
      label: t('pane.designLibraryTab.renameDesign'),
      onSelect: () => {
        setDraft(artifact.title);
        setRenaming(true);
      },
    },
    {
      kind: TW_MENU_KIND.action,
      id: 'dialog',
      label: t('pane.designLibraryTab.continueDialog'),
      onSelect: () => void startDialog(),
    },
    {
      kind: TW_MENU_KIND.action,
      id: 'share',
      label: t('pane.designLibraryTab.shareLink'),
      icon: <Share2 />,
      onSelect: () => void share({ project: artifact.id, board: null, full: false }, artifact.title),
    },
    ...(first
      ? ([
          {
            kind: TW_MENU_KIND.action,
            id: 'phone',
            label: t('pane.designLibraryTab.sendToPhone'),
            onSelect: () =>
              void sendToPhone({ artifact: artifact.id, board: first.path, title: first.title }),
          },
        ] as TTwMenuEntry[])
      : []),
    {
      kind: TW_MENU_KIND.action,
      id: 'path',
      label: copied ? t('pane.designLibraryTab.pathCopied') : t('pane.designLibraryTab.copyPath'),
      onSelect: () => void copyPath(),
    },
  ];

  return (
    <article className="tw-dlib-card" data-open={expanded || undefined}>
      <div className="tw-dlib-card-head">
        <button
          type="button"
          className="tw-dlib-cover"
          aria-label={t('pane.designLibraryTab.open', { title: artifact.title })}
          disabled={!first}
          onClick={() =>
            first && onOpenBoard(libraryBoardAddress(port, artifact.id, first.path), first.title)
          }
        >
          {artifact.cover ? (
            <img src={scopedAppPath(artifact.cover, currentMachineId())} alt="" loading="lazy" />
          ) : (
            <span>{initials(artifact.title)}</span>
          )}
        </button>
        <div className="tw-dlib-card-titles">
          {renaming ? (
            <input
              className="tw-dlib-filter"
              value={draft}
              autoFocus
              maxLength={120}
              aria-label={t('pane.designLibraryTab.newDesignName')}
              onChange={(event) => setDraft(event.target.value)}
              onBlur={() => void saveTitle()}
              onKeyDown={(event) => {
                if (event.key === 'Enter') void saveTitle();
                if (event.key === 'Escape') setRenaming(false);
              }}
            />
          ) : (
            <h4 className="tw-dlib-card-title" title={artifact.title}>
              {artifact.title}
            </h4>
          )}
          <p className="tw-dlib-card-meta">
            {boardsLabel(artifact.boards.length)}
            {artifact.updatedAt > 0 && ` · ${when(artifact.updatedAt)}`}
            {artifact.claims.length > 0 && ` · ${claimsLine(artifact.claims)}`}
            {artifact.check && artifact.check.finishedAt !== null && artifact.check.broken > 0 && (
              <button
                type="button"
                className="tw-dlib-flag tw-dlib-flag-error"
                onClick={() => setExpanded(true)}
              >
                {t('pane.designLibraryTab.withErrors', { n: artifact.check.broken })}
              </button>
            )}
          </p>
        </div>
        {artifact.agents.length > 0 && (
          <button
            type="button"
            className="tw-dlib-faces"
            aria-expanded={expanded}
            title={t('pane.designLibraryTab.whoWorks')}
            onClick={() => setExpanded((open) => !open)}
          >
            {(me ? [me, ...others] : others).slice(0, 3).map((agent) => (
              <span
                key={agent.tabId}
                className="tw-dlib-face"
                data-state={agent.state}
                data-self={agent.self || undefined}
              >
                {agent.self ? t('pane.designLibraryTab.me') : initials(agent.title)}
              </span>
            ))}
            {artifact.agents.length > 3 && (
              <span className="tw-dlib-face-more">+{artifact.agents.length - 3}</span>
            )}
          </button>
        )}
        <TwButton
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.secondary}
          icon={starting ? <TwSpinner /> : <Eye />}
          disabled={!first}
          onClick={() =>
            first && onOpenBoard(libraryBoardAddress(port, artifact.id, first.path), first.title)
          }
        >
          {t('pane.designLibraryTab.openButton')}
        </TwButton>
        <TwMenuButton
          items={menu}
          menuLabel={artifact.title}
          align={LAYER_ALIGN.end}
          className="tw-icon-button"
          aria-label={t('pane.designLibraryTab.moreFor', { title: artifact.title })}
          title={t('pane.designLibraryTab.more')}
        >
          <MoreHorizontal />
        </TwMenuButton>
      </div>
      {note && (
        <p
          className={cx('tw-dlib-note', !note.ok && 'tw-dlib-note-error')}
          role={note.ok ? 'status' : 'alert'}
        >
          {note.text}
        </p>
      )}
      {expanded && (
        <div className="tw-dlib-details">
          {artifact.agents.length > 0 && (
            <ul className="tw-dlib-agents" aria-label={t('pane.designLibraryTab.whoWorks')}>
              {artifact.agents.map((agent) => (
                <AgentRow key={agent.tabId} agent={agent} artifact={artifact} />
              ))}
            </ul>
          )}
          <BoardList artifact={artifact} port={port} onOpenBoard={onOpenBoard} onChanged={onChanged} />
          <DesignCheckPanel artifact={artifact} port={port} onOpenBoard={onOpenBoard} autoStart={checkNow} />
        </div>
      )}
    </article>
  );
};

interface ISectionProps {
  title: string;
  count: number;
  children: ReactNode;
}

const Section = ({ title, count, children }: ISectionProps) => (
  <section className="tw-dlib-section" aria-label={title}>
    <h4 className="tw-dlib-section-title">
      {title}
      {count > 0 && <span className="tw-dlib-section-count">{count}</span>}
    </h4>
    {children}
  </section>
);

type TUpload =
  | { kind: 'idle' }
  | { kind: 'sending'; name: string }
  | { kind: 'done'; message: string }
  | { kind: 'error'; message: string };

const isZip = (file: File): boolean => /\.zip$/i.test(file.name) || file.type === 'application/zip';

interface IDesignLibraryTabProps {
  library: TDesignLibrary;
  /** Opens a page of the library server in the panel's browser, where it can be picked and commented on. */
  onOpenBoard: TOpenBoard;
}

/**
 * «Claude Design» in the right panel: the designs this conversation's agent works on, those other agents
 * work on (open their conversation or write to them from here), and the rest of the library. A board opens in the
 * panel's browser with the element picker; the page reloads by itself when an agent edits the files. A Claude Design
 * export dropped on the tab joins the library.
 */
const DesignLibraryTab = ({ library, onOpenBoard }: IDesignLibraryTabProps) => {
  const { state, refresh } = library;
  const [upload, setUpload] = useState<TUpload>({ kind: 'idle' });
  const [dragging, setDragging] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const groups = useMemo(() => groupArtifacts(state.kind === 'ready' ? state.data.artifacts : []), [state]);

  const send = async (file: File) => {
    if (upload.kind === 'sending') return;
    if (!isZip(file)) {
      setUpload({ kind: 'error', message: t('pane.designLibraryTab.needZip') });
      return;
    }
    setUpload({ kind: 'sending', name: file.name });
    try {
      const result = await uploadDesignProject(currentMachineId(), file);
      setUpload({
        kind: 'done',
        message: t('pane.designLibraryTab.uploaded', {
          title: result.title,
          boards: boardsLabel(result.boards),
        }),
      });
      await refresh();
    } catch (error) {
      setUpload({
        kind: 'error',
        message: error instanceof Error ? error.message : t('pane.designLibraryTab.uploadFailed'),
      });
    }
  };
  const hasFiles = (event: DragEvent) => Array.from(event.dataTransfer.types).includes('Files');
  const onDragOver = (event: DragEvent<HTMLDivElement>) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    setDragging(true);
  };
  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    setDragging(false);
    const file = event.dataTransfer.files[0];
    if (file) void send(file);
  };

  if (state.kind === 'loading') {
    return (
      <div className="tw-rpane-empty" role="status">
        <TwSpinner />
      </div>
    );
  }
  if (state.kind === 'error') {
    return (
      <div className="tw-rpane-empty" role="alert">
        <LayoutTemplate />
        <strong>{t('pane.designLibraryTab.unavailable')}</strong>
        <p>{state.message}</p>
      </div>
    );
  }

  const { port, root } = state.data;
  const card = (artifact: ILibraryArtifact) => (
    <ArtifactCard
      key={artifact.id}
      artifact={artifact}
      port={port}
      onOpenBoard={onOpenBoard}
      onChanged={() => void refresh()}
    />
  );
  return (
    <div
      className="tw-dlib"
      data-dragging={dragging || undefined}
      onDragOver={onDragOver}
      onDragLeave={() => setDragging(false)}
      onDrop={onDrop}
    >
      <header className="tw-dlib-head">
        <div className="tw-dlib-heading">
          <h3 className="tw-dlib-title">Claude Design</h3>
        </div>
        <div className="tw-dlib-head-actions">
          <TwTooltip content={t('pane.designLibraryTab.uploadHint')}>
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              icon={<Upload />}
              pending={upload.kind === 'sending'}
              aria-label={t('pane.designLibraryTab.uploadZip')}
              onClick={() => input.current?.click()}
            />
          </TwTooltip>
          <TwTooltip content={t('pane.designLibraryTab.wholeFolderHint')}>
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              icon={<FolderOpen />}
              aria-label={t('pane.designLibraryTab.wholeFolder')}
              onClick={() => onOpenBoard(`localhost:${port}/`, 'Claude Design')}
            />
          </TwTooltip>
        </div>
        <input
          ref={input}
          type="file"
          accept=".zip,application/zip"
          className="tw-dlib-file"
          tabIndex={-1}
          aria-hidden="true"
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = '';
            if (file) void send(file);
          }}
        />
      </header>
      <p className="tw-rpane-muted">{t('pane.designLibraryTab.intro')}</p>
      {upload.kind === 'sending' && (
        <p className="tw-dlib-note" role="status">
          {t('pane.designLibraryTab.uploading', { name: upload.name })}
        </p>
      )}
      {upload.kind === 'done' && (
        <p className="tw-dlib-note" role="status">
          {upload.message}
        </p>
      )}
      {upload.kind === 'error' && (
        <p className="tw-dlib-note tw-dlib-note-error" role="alert">
          {upload.message}
        </p>
      )}

      <Section title={t('pane.designLibraryTab.thisAgent')} count={groups.mine.length}>
        {groups.mine.length > 0 ? (
          groups.mine.map(card)
        ) : (
          <p className="tw-rpane-muted">{t('pane.designLibraryTab.mineEmpty')}</p>
        )}
      </Section>
      {groups.others.length > 0 && (
        <Section title={t('pane.designLibraryTab.otherAgents')} count={groups.others.length}>
          {groups.others.map(card)}
        </Section>
      )}
      <Section title={t('pane.designLibraryTab.allDesigns')} count={groups.rest.length}>
        {groups.rest.length > 0 ? (
          groups.rest.map(card)
        ) : (
          <p className="tw-rpane-muted">{t('pane.designLibraryTab.restEmpty')}</p>
        )}
      </Section>
      <p className="tw-rpane-muted tw-dlib-root">
        {t('pane.designLibraryTab.folder')} <code>{root}</code>
      </p>
      {dragging && (
        <div className="tw-dlib-drop" aria-hidden="true">
          {t('pane.designLibraryTab.dropHint')}
        </div>
      )}
    </div>
  );
};

interface IDesignIslandProps {
  library: TDesignLibrary;
  onOpenBoard: TOpenBoard;
  onOpenTab: () => void;
}

/** The overview's island: this agent's designs at a glance, what the others have, and the way into the tab. */
export const DesignIsland = ({ library, onOpenBoard, onOpenTab }: IDesignIslandProps) => {
  const { state } = library;
  if (state.kind !== 'ready')
    return state.kind === 'loading' ? <TwSpinner /> : <p className="tw-rpane-muted">{state.message}</p>;
  const { port, artifacts } = state.data;
  const { mine, others } = groupArtifacts(artifacts);
  const busyElsewhere = others.filter((artifact) =>
    artifact.agents.some((agent) => agent.state === 'busy'),
  ).length;
  return (
    <div className="tw-dlib-island">
      {mine.length === 0 && (
        <p className="tw-rpane-muted">{t('pane.designLibraryTab.islandEmpty', { n: artifacts.length })}</p>
      )}
      {mine.slice(0, 3).map((artifact) => {
        const first = mainBoard(artifact);
        const me = artifact.agents.find((agent) => agent.self);
        return (
          <div key={artifact.id} className="tw-dlib-island-row">
            <span className="tw-dlib-agent-dot" data-state={me?.state ?? 'idle'} aria-hidden="true" />
            <span className="tw-dlib-island-title" title={artifact.title}>
              {artifact.title}
            </span>
            <span className="tw-dlib-island-meta">{boardsLabel(artifact.boards.length)}</span>
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              icon={<Eye />}
              disabled={!first}
              onClick={() =>
                first && onOpenBoard(libraryBoardAddress(port, artifact.id, first.path), first.title)
              }
            >
              {t('pane.designLibraryTab.openButton')}
            </TwButton>
          </div>
        );
      })}
      <div className="tw-dlib-island-foot">
        <button type="button" className="tw-dlib-link" onClick={onOpenTab}>
          {others.length > 0
            ? t('pane.designLibraryTab.others', { n: others.length }) +
              (busyElsewhere ? t('pane.designLibraryTab.othersBusy', { n: busyElsewhere }) : '')
            : t('pane.designLibraryTab.allDesigns')}
          <ChevronRight className="tw-dlib-chevron" />
        </button>
      </div>
      {mine.length === 0 && artifacts.length > 0 && (
        <button
          type="button"
          className="tw-dlib-link"
          onClick={() => onOpenBoard(libraryArtifactAddress(port, artifacts[0].id), artifacts[0].title)}
        >
          {t('pane.designLibraryTab.openLatest', { title: artifacts[0].title })}
        </button>
      )}
    </div>
  );
};

export default DesignLibraryTab;
