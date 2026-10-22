import { useState } from 'react';
import { ArrowUpCircle, ExternalLink, MessageSquare, MessageSquarePlus, Share2 } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  TW_MENU_KIND,
  TwButton,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import { BackLink, GalleryCard, GalleryGrid, Note } from '@/components/agents/design/library/library-parts';
import { ShareButton } from '@/components/agents/design/library/share-button';
import useDesignLink from '@/hooks/use-design-link';
import { scopedAppPath } from '@/lib/app-path';
import { continueDesignDialog, upgradeDesignRelease } from '@/lib/hl-design-library-client';
import type { ILibraryProject } from '@/lib/hl-design-library/fleet';
import { boardClaimCaption, claimOfBoard } from '@/lib/hl-design-library/home-model';
import {
  libraryThumbPath,
  type ILibraryBoard,
  type ILibraryProjectRelease,
} from '@/lib/hl-design-library/types';
import { OPEN_MODE, chooseOpenMode, readDeviceEnv } from '@/lib/hl-design-link';
import { openConversation } from '@/lib/hl-open-conversation';
import { designChatMachine, latestDesignChat } from '@/lib/new-task-machine';
import { t } from '@/lib/i18n';

interface IProjectPageProps {
  project: ILibraryProject;
  onBack: () => void;
  onOpenBoard: (path: string) => void;
}

/** Level two: the boards of one project as pictures with names, and a way into a chat in the project. */
export const ProjectPage = ({ project, onBack, onOpenBoard }: IProjectPageProps) => {
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [upgrading, setUpgrading] = useState(false);
  /** The release the runtime answered with after "Upgrade": the listing catches up on its next poll. */
  const [moved, setMoved] = useState<ILibraryProjectRelease | null>(null);
  const upgradeTo = (moved ?? project.release)?.upgrade ?? null;
  const { share, openFull } = useDesignLink();
  const where = project.machineId ? { machine: project.machineId } : {};

  const boardMenu = (board: ILibraryBoard): TTwMenuEntry[] => {
    const location = { project: project.id, board: board.path, full: false, ...where };
    return [
      {
        kind: TW_MENU_KIND.action,
        id: 'share',
        label: t('design.projectPage.share'),
        icon: <Share2 />,
        onSelect: () => void share(location, board.title),
      },
      ...(chooseOpenMode(readDeviceEnv()) === OPEN_MODE.window
        ? ([
            {
              kind: TW_MENU_KIND.action,
              id: 'tab',
              label: t('design.projectPage.openNewTab'),
              icon: <ExternalLink />,
              onSelect: () => openFull(location),
            },
          ] as TTwMenuEntry[])
        : []),
    ];
  };

  const existing = latestDesignChat(project);

  /** "Open in chat" returns to the latest conversation about the design; with none yet it starts one. */
  const openChat = () => {
    if (existing) openConversation(existing.agent.tabId, existing.machineId);
    else void newChat();
  };

  /** "New dialog": always a fresh conversation in the design's folder, on the machine picked for new tasks. */
  const newChat = async () => {
    setOpening(true);
    setError(null);
    try {
      const machine = designChatMachine(project);
      const { tabId } = await continueDesignDialog(machine, project.id);
      openConversation(tabId, machine);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('design.projectPage.chatFailed'));
    } finally {
      setOpening(false);
    }
  };

  /** "Upgrade to release ...": only after the runtime confirms is the project shown on the new release. */
  const upgrade = async (target: string) => {
    setUpgrading(true);
    setError(null);
    try {
      const answer = await upgradeDesignRelease(project.machineId, project.id, target);
      if (answer.project) setMoved(answer.project);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('design.projectPage.upgradeFailed'));
    } finally {
      setUpgrading(false);
    }
  };

  return (
    <div className="tw-dhome-scroll">
      <header className="tw-dhome-head">
        <div className="tw-dhome-heading">
          <BackLink label="Claude Design" onClick={onBack} />
          <h1 className="tw-dhome-title">{project.title}</h1>
        </div>
        <div className="tw-dhome-actions">
          {upgradeTo && (
            <TwButton
              size={BUTTON_SIZE.md}
              variant={BUTTON_VARIANT.ghost}
              icon={<ArrowUpCircle />}
              pending={upgrading}
              onClick={() => void upgrade(upgradeTo)}
            >
              {t('design.projectPage.upgrade', { release: upgradeTo })}
            </TwButton>
          )}
          <ShareButton
            size={BUTTON_SIZE.md}
            location={{ project: project.id, board: null, full: false, ...where }}
            title={project.title}
          />
          {existing && (
            <TwButton
              size={BUTTON_SIZE.md}
              variant={BUTTON_VARIANT.ghost}
              icon={<MessageSquarePlus />}
              pending={opening}
              onClick={() => void newChat()}
            >
              {t('design.projectPage.newDialog')}
            </TwButton>
          )}
          <TwButton
            size={BUTTON_SIZE.md}
            variant={BUTTON_VARIANT.secondary}
            icon={<MessageSquare />}
            pending={opening && !existing}
            onClick={openChat}
          >
            {t('design.projectPage.openInChat')}
          </TwButton>
        </div>
      </header>

      {error && <Note error>{error}</Note>}
      {moved && <Note>{t('design.projectPage.upgraded', { release: moved.current })}</Note>}
      {project.boards.length === 0 ? (
        <div className="tw-dhome-empty">
          <strong>{t('design.projectPage.noBoards')}</strong>
          <p>{t('design.projectPage.noBoardsHint')}</p>
        </div>
      ) : (
        <GalleryGrid label={t('design.projectPage.boards')}>
          {project.boards.map((board) => (
            <li key={board.path}>
              <GalleryCard
                title={board.title}
                caption={boardClaimCaption(claimOfBoard(project.claims, board))}
                src={scopedAppPath(
                  libraryThumbPath(project.id, board.path, project.updatedAt, true),
                  project.machineId,
                )}
                fallback={board.title.slice(0, 1).toUpperCase()}
                onOpen={() => onOpenBoard(board.path)}
                menu={boardMenu(board)}
              />
            </li>
          ))}
        </GalleryGrid>
      )}
    </div>
  );
};
