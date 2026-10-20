import { useMemo, useState } from 'react';
import { ChevronLeft, ExternalLink, Maximize2, Send, X } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  TwButton,
  TwSelect,
  TwTooltip,
  type ITwOption,
} from '@/components/agents/ui';
import { BoardHistory } from '@/components/agents/design/library/board-history';
import { BackLink, LibraryNotFound, Note } from '@/components/agents/design/library/library-parts';
import { ShareButton } from '@/components/agents/design/library/share-button';
import { PREVIEW_KEY, useBoardFrame } from '@/components/agents/design/library/use-board-frame';
import { PreviewBody, reopenTarget } from '@/components/agents/inspector/preview-island';
import useDesignLink from '@/hooks/use-design-link';
import { annotationLabel, appendAnnotations } from '@/lib/hl-annotation-format';
import { clearAnnotations, removeAnnotation, useAnnotations } from '@/lib/hl-annotations-store';
import { continueDesignDialog, messageDesignAgent } from '@/lib/hl-design-library-client';
import { isBoardFile } from '@/lib/hl-design-library/board-detect';
import { AGENT_STATE_LABEL, isAgentRunning } from '@/lib/hl-design-library/group';
import { claimOfBoard, NEW_CHAT, resolveTarget } from '@/lib/hl-design-library/home-model';
import type { ILibraryProject } from '@/lib/hl-design-library/fleet';
import { libraryBoardAddress, type IArtifactAgent } from '@/lib/hl-design-library/types';
import { OPEN_MODE, chooseOpenMode, readDeviceEnv } from '@/lib/hl-design-link';
import { openConversation } from '@/lib/hl-open-conversation';
import { t } from '@/lib/i18n';

type TDelivery =
  | { kind: 'sent'; tabId: string; title: string }
  | { kind: 'started'; tabId: string }
  | { kind: 'error'; message: string };

interface IStartedChat {
  tabId: string;
  title: string;
  at: number;
}

interface IBoardPageProps {
  project: ILibraryProject;
  boardPath: string;
  onOpenProject: () => void;
  onOpenProjects: () => void;
  onOpenBoard: (path: string) => void;
  /** The board alone in the window. */
  onOpenFull: () => void;
}

/**
 * Level three: one board full size under a single slim toolbar, with the element picker and the comment of the browser
 * tab. There is no chat in the centre to take the comments, so a picker names the chat they go to: one that worked on
 * the project, or a new one.
 */
export const BoardPage = ({
  project,
  boardPath,
  onOpenProject,
  onOpenProjects,
  onOpenBoard,
  onOpenFull,
}: IBoardPageProps) => {
  const board = project.boards.find((item) => item.path === boardPath) ?? null;
  const address = board ? libraryBoardAddress(project.port, project.id, board.path) : null;
  const { state, patch, open } = useBoardFrame(address, project.machineId);
  const { openFull } = useDesignLink();
  const annotations = useAnnotations(PREVIEW_KEY);
  const [chosen, setChosen] = useState<string | null>(null);
  const [started, setStarted] = useState<IStartedChat[]>([]);
  const [sending, setSending] = useState(false);
  const [delivery, setDelivery] = useState<TDelivery | null>(null);

  const agents = useMemo<IArtifactAgent[]>(
    () => [
      ...project.agents,
      ...started
        .filter((chat) => !project.agents.some((agent) => agent.tabId === chat.tabId))
        .map((chat): IArtifactAgent => ({
          tabId: chat.tabId,
          title: chat.title,
          workspace: null,
          state: 'busy',
          lastEventAt: chat.at,
          self: false,
        })),
    ],
    [project.agents, started],
  );
  const target = resolveTarget(chosen, agents);
  const targetOptions = useMemo<ITwOption[]>(
    () => [
      ...agents.map((agent) => ({
        value: agent.tabId,
        label: agent.title,
        meta: AGENT_STATE_LABEL[agent.state],
        disabled: !isAgentRunning(agent.state),
      })),
      { value: NEW_CHAT, label: t('design.boardPage.newChat') },
    ],
    [agents],
  );
  const boardOptions = useMemo<ITwOption[]>(
    () => project.boards.map((item) => ({ value: item.path, label: item.title })),
    [project.boards],
  );

  if (!board) {
    return (
      <div className="tw-dhome-scroll">
        <header className="tw-dhome-head">
          <BackLink label={project.title} onClick={onOpenProject} />
        </header>
        <LibraryNotFound
          what={t('design.boardPage.boardGone')}
          onProjects={onOpenProjects}
          onProject={onOpenProject}
        />
      </div>
    );
  }

  const targetTitle = agents.find((agent) => agent.tabId === target)?.title ?? t('design.boardPage.chat');
  const machine = project.machineId;

  /** Pessimistic: the comments stay until the runtime has taken them. */
  const sendComments = async () => {
    const blocks = appendAnnotations('', annotations);
    setSending(true);
    setDelivery(null);
    try {
      if (target === NEW_CHAT) {
        const { tabId } = await continueDesignDialog(
          machine,
          project.id,
          t('design.boardPage.editsPrompt', { title: board.title, path: board.path, blocks }),
        );
        setStarted((chats) => [
          ...chats,
          { tabId, title: t('design.boardPage.chatTitle', { title: board.title }), at: Date.now() },
        ]);
        setChosen(tabId);
        setDelivery({ kind: 'started', tabId });
      } else {
        await messageDesignAgent(machine, target, project.id, blocks, { blocks: true });
        setDelivery({ kind: 'sent', tabId: target, title: targetTitle });
      }
      clearAnnotations(PREVIEW_KEY);
    } catch (failure) {
      setDelivery({
        kind: 'error',
        message: failure instanceof Error ? failure.message : t('design.boardPage.sendFailed'),
      });
    } finally {
      setSending(false);
    }
  };

  const leading = (
    <>
      <TwTooltip content={project.title}>
        <TwButton
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          icon={<ChevronLeft />}
          aria-label={t('design.boardPage.toProject', { title: project.title })}
          onClick={onOpenProject}
        />
      </TwTooltip>
      <TwSelect
        aria-label={t('design.boardPage.board')}
        className="tw-dhome-board-select"
        options={boardOptions}
        value={board.path}
        onChange={onOpenBoard}
      />
    </>
  );
  const commentsTo = (
    <div className="tw-dhome-target">
      <span className="tw-dhome-target-label">{t('design.boardPage.commentsTo')}</span>
      <TwSelect
        aria-label={t('design.boardPage.commentsChat')}
        className="tw-dhome-target-select"
        options={targetOptions}
        value={target}
        disabled={sending}
        onChange={setChosen}
      />
    </div>
  );
  const frameOpen = state.kind === 'open';
  const here = { project: project.id, board: board.path, full: false, ...(machine ? { machine } : {}) };
  const newWindow = chooseOpenMode(readDeviceEnv()) === OPEN_MODE.window;
  const trailing = (
    <>
      {isBoardFile(board.path) && (
        <BoardHistory
          key={board.path}
          project={project}
          board={board}
          onRestored={() => {
            if (state.kind === 'open') void open(reopenTarget(state), []);
          }}
        />
      )}
      <ShareButton location={here} title={board.title} />
      <TwTooltip content={t('design.boardPage.fullscreen')}>
        <TwButton
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          icon={<Maximize2 />}
          aria-label={t('design.boardPage.fullscreen')}
          onClick={onOpenFull}
        />
      </TwTooltip>
      {newWindow && (
        <TwTooltip content={t('design.boardPage.openNewBrowserTab')}>
          <TwButton
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.ghost}
            icon={<ExternalLink />}
            aria-label={t('design.boardPage.openNewTab')}
            onClick={() => openFull(here)}
          />
        </TwTooltip>
      )}
      {commentsTo}
    </>
  );
  const claim = claimOfBoard(project.claims, board);
  const framed =
    state.kind === 'open' || state.kind === 'opening' || state.kind === 'taken' || state.kind === 'error';

  return (
    <div className="tw-dhome-board-page">
      {!frameOpen && (
        <div className="tw-preview-toolbar" role="toolbar" aria-label={t('design.boardPage.board')}>
          {leading}
          <span className="tw-dhome-frame-spacer" />
          {trailing}
        </div>
      )}
      {claim && (
        <p className="tw-dhome-claim" role="status">
          <span>{t('design.boardPage.claim', { person: claim.person, chat: claim.chatTitle })}</span>
          {claim.local && (
            <button type="button" className="tw-dlib-link" onClick={() => openConversation(claim.tabId)}>
              {t('design.boardPage.openChat')}
            </button>
          )}
        </p>
      )}
      <div className="tw-dhome-board-stage">
        {!framed && (
          <div
            className="tw-preview-frame tw-skeleton"
            role="status"
            aria-label={t('design.boardPage.openingBoard')}
          />
        )}
        <PreviewBody
          previewKey={PREVIEW_KEY}
          state={state}
          onReopenTaken={(taken) => void open(reopenTarget(taken), [])}
          onStale={(current) => void open(reopenTarget(current), [])}
          onEntered={(current) => patch({ state: { ...current, entered: true } })}
          onReload={(current) => void open(reopenTarget(current), [])}
          onOpenUrl={(to) => void open(to, [])}
          leading={leading}
          hideHistory
          pickTip={t('design.boardPage.pickTip')}
          address={() => <span className="tw-dhome-frame-spacer" />}
          actions={trailing}
        />
      </div>
      {(annotations.length > 0 || delivery !== null) && (
        <section className="tw-dhome-tray" aria-label={t('design.boardPage.comments')}>
          {annotations.length > 0 && (
            <>
              <ul className="tw-dhome-annotations">
                {annotations.map((annotation) => (
                  <li key={annotation.id} className="tw-dhome-annotation">
                    <span className="tw-dhome-annotation-text">
                      <strong>{annotationLabel(annotation)}</strong>
                      {annotation.comment && <span>{annotation.comment}</span>}
                    </span>
                    <TwButton
                      size={BUTTON_SIZE.sm}
                      variant={BUTTON_VARIANT.ghost}
                      icon={<X />}
                      aria-label={t('design.boardPage.removeComment', { label: annotationLabel(annotation) })}
                      disabled={sending}
                      onClick={() => removeAnnotation(PREVIEW_KEY, annotation.id)}
                    />
                  </li>
                ))}
              </ul>
              <TwButton
                size={BUTTON_SIZE.md}
                variant={BUTTON_VARIANT.primary}
                icon={<Send />}
                pending={sending}
                onClick={() => void sendComments()}
              >
                {target === NEW_CHAT ? t('design.boardPage.startChat') : t('design.boardPage.send')}
              </TwButton>
            </>
          )}
          {delivery?.kind === 'sent' && (
            <Note>
              {t('design.boardPage.sentTo', { title: delivery.title })}{' '}
              <button type="button" className="tw-dlib-link" onClick={() => openConversation(delivery.tabId)}>
                {t('design.boardPage.openChat')}
              </button>
            </Note>
          )}
          {delivery?.kind === 'started' && (
            <Note>
              {t('design.boardPage.chatStarted')}{' '}
              <button type="button" className="tw-dlib-link" onClick={() => openConversation(delivery.tabId)}>
                {t('design.boardPage.openChat')}
              </button>
            </Note>
          )}
          {delivery?.kind === 'error' && <Note error>{delivery.message}</Note>}
        </section>
      )}
    </div>
  );
};
