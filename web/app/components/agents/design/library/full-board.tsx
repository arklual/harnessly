import { useState, useSyncExternalStore } from 'react';
import Head from 'next/head';
import { toast } from 'sonner';
import { ChevronDown, ChevronLeft, ChevronUp, Maximize, Minimize } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton, TwLayerHostContext, TwTooltip } from '@/components/agents/ui';
import { LibraryNotFound } from '@/components/agents/design/library/library-parts';
import { ShareButton } from '@/components/agents/design/library/share-button';
import { PREVIEW_KEY, useBoardFrame } from '@/components/agents/design/library/use-board-frame';
import { PreviewBody, reopenTarget } from '@/components/agents/inspector/preview-island';
import type { ILibraryProject } from '@/lib/hl-design-library/fleet';
import { libraryBoardAddress } from '@/lib/hl-design-library/types';
import { t } from '@/lib/i18n';

const noSubscription = () => () => {};

const subscribeFullscreen = (notify: () => void) => {
  document.addEventListener('fullscreenchange', notify);
  return () => document.removeEventListener('fullscreenchange', notify);
};

/** The Fullscreen API on one element: a computer has it, an iPhone's Safari has it for video only, and there the window itself is the whole screen. */
const useFullscreen = (container: HTMLElement | null) => {
  const available = useSyncExternalStore(
    noSubscription,
    () => document.fullscreenEnabled === true,
    () => false,
  );
  const active = useSyncExternalStore(
    subscribeFullscreen,
    () => container !== null && document.fullscreenElement === container,
    () => false,
  );
  const toggle = async () => {
    try {
      if (active) await document.exitFullscreen();
      else await container?.requestFullscreen();
    } catch {
      toast.error(t('design.fullBoard.fullscreenDenied'));
    }
  };
  return { available, active, toggle };
};

interface IFullBoardProps {
  project: ILibraryProject;
  boardPath: string;
  onClose: () => void;
  onOpenProjects: () => void;
}

/**
 * The board alone in the window, the same preview with the element picker, under a small floating bar: the way
 * back, the name, the link to share, the screen of the browser. The address of this place is a link of its own, so it
 * opens in a new tab, on a phone, and for a teammate who is sent it.
 */
export const FullBoard = ({ project, boardPath, onClose, onOpenProjects }: IFullBoardProps) => {
  const board = project.boards.find((item) => item.path === boardPath) ?? null;
  const { state, patch, open } = useBoardFrame(
    board ? libraryBoardAddress(project.port, project.id, board.path) : null,
    project.machineId,
  );
  const [container, setContainer] = useState<HTMLElement | null>(null);
  const [folded, setFolded] = useState(false);
  const fullscreen = useFullscreen(container);

  if (!board) {
    return (
      <div className="tw-dhome tw-dhome-window">
        <LibraryNotFound
          what={t('design.fullBoard.boardGone')}
          onProjects={onOpenProjects}
          onProject={onClose}
        />
      </div>
    );
  }

  const frameOpen = state.kind === 'open';
  const framed = frameOpen || state.kind === 'opening' || state.kind === 'taken' || state.kind === 'error';
  const leading = (
    <>
      <TwTooltip content={t('design.fullBoard.backToBoard')}>
        <TwButton
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          icon={<ChevronLeft />}
          aria-label={t('design.fullBoard.backToBoard')}
          onClick={onClose}
        />
      </TwTooltip>
      <span className="tw-dhome-full-title" title={board.title}>
        {board.title}
      </span>
    </>
  );
  const actions = (
    <>
      <ShareButton
        location={{
          project: project.id,
          board: board.path,
          full: false,
          ...(project.machineId ? { machine: project.machineId } : {}),
        }}
        title={board.title}
      />
      {fullscreen.available && (
        <TwTooltip
          content={
            fullscreen.active ? t('design.fullBoard.exitFullscreen') : t('design.fullBoard.fullscreen')
          }
        >
          <TwButton
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.ghost}
            icon={fullscreen.active ? <Minimize /> : <Maximize />}
            aria-label={
              fullscreen.active ? t('design.fullBoard.exitFullscreen') : t('design.fullBoard.fullscreen')
            }
            onClick={() => void fullscreen.toggle()}
          />
        </TwTooltip>
      )}
      <TwTooltip content={folded ? t('design.fullBoard.showPanel') : t('design.fullBoard.foldPanel')}>
        <TwButton
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          className="tw-dhome-full-fold"
          icon={folded ? <ChevronDown /> : <ChevronUp />}
          aria-label={folded ? t('design.fullBoard.showPanel') : t('design.fullBoard.foldPanel')}
          aria-expanded={!folded}
          onClick={() => setFolded((value) => !value)}
        />
      </TwTooltip>
    </>
  );

  return (
    <TwLayerHostContext.Provider value={container}>
      <div ref={setContainer} className="tw-dhome tw-dhome-full" data-folded={folded || undefined}>
        <Head>
          <title>{`${board.title} · Claude Design`}</title>
        </Head>
        <div className="tw-dhome-board-stage">
          {!frameOpen && (
            <div className="tw-preview-toolbar" role="toolbar" aria-label={t('design.fullBoard.board')}>
              {leading}
              {actions}
            </div>
          )}
          {!framed && (
            <div
              className="tw-preview-frame tw-skeleton"
              role="status"
              aria-label={t('design.fullBoard.openingBoard')}
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
            pickTip={t('design.fullBoard.pickTip')}
            address={() => null}
            actions={actions}
          />
        </div>
      </div>
    </TwLayerHostContext.Provider>
  );
};
