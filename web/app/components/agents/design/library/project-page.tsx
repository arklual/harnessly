import { useState } from 'react';
import { ExternalLink, MessageSquare, Share2 } from 'lucide-react';
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
import { continueDesignDialog } from '@/lib/hl-design-library-client';
import type { ILibraryProject } from '@/lib/hl-design-library/fleet';
import { boardClaimCaption, claimOfBoard } from '@/lib/hl-design-library/home-model';
import { libraryThumbPath, type ILibraryBoard } from '@/lib/hl-design-library/types';
import { OPEN_MODE, chooseOpenMode, readDeviceEnv } from '@/lib/hl-design-link';
import { openConversation } from '@/lib/hl-open-conversation';
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

  const openChat = async () => {
    setOpening(true);
    setError(null);
    try {
      const { tabId } = await continueDesignDialog(project.machineId, project.id);
      openConversation(tabId);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('design.projectPage.chatFailed'));
    } finally {
      setOpening(false);
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
          <ShareButton
            size={BUTTON_SIZE.md}
            location={{ project: project.id, board: null, full: false, ...where }}
            title={project.title}
          />
          <TwButton
            size={BUTTON_SIZE.md}
            variant={BUTTON_VARIANT.secondary}
            icon={<MessageSquare />}
            pending={opening}
            onClick={() => void openChat()}
          >
            {t('design.projectPage.openInChat')}
          </TwButton>
        </div>
      </header>

      {error && <Note error>{error}</Note>}
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
