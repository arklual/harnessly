import { LayoutTemplate } from 'lucide-react';
import { BoardPage } from '@/components/agents/design/library/board-page';
import { FullBoard } from '@/components/agents/design/library/full-board';
import { BackLink, GalleryGrid, LibraryNotFound } from '@/components/agents/design/library/library-parts';
import { ProjectPage } from '@/components/agents/design/library/project-page';
import { ProjectsGrid } from '@/components/agents/design/library/projects-grid';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton } from '@/components/agents/ui';
import { useFleetDesignLibrary } from '@/hooks/use-design-library';
import useDesignUploads from '@/hooks/use-design-uploads';
import { DESIGN_HOME, type IDesignLocation } from '@/lib/hl-conversation-url';
import { findProject } from '@/lib/hl-design-library/fleet';
import { t } from '@/lib/i18n';

interface ILibraryHomeProps {
  location: IDesignLocation;
  onNavigate: (next: IDesignLocation) => void;
}

/**
 * «Claude Design» of the left menu: the shared library of the team in the centre, in three levels — the
 * projects, one project's boards, one board full size. The place is in the address, so reload and Back work.
 * It reads the libraries of all the person's machines; no conversation is needed.
 */
const LibraryHome = ({ location, onNavigate }: ILibraryHomeProps) => {
  const { state, refresh } = useFleetDesignLibrary();
  const uploads = useDesignUploads(refresh);

  if (state.kind === 'error') {
    return (
      <div className="tw-dhome">
        <div className="tw-dhome-empty" role="alert">
          <LayoutTemplate aria-hidden="true" />
          <strong>{t('design.libraryHome.loadFailed')}</strong>
          <TwButton size={BUTTON_SIZE.md} variant={BUTTON_VARIANT.secondary} onClick={() => void refresh()}>
            {t('design.libraryHome.retry')}
          </TwButton>
        </div>
      </div>
    );
  }

  const projects = state.kind === 'ready' ? state.projects : null;
  const toProjects = () => onNavigate(DESIGN_HOME);

  if (location.project === null) {
    return (
      <div className="tw-dhome">
        <ProjectsGrid
          projects={projects}
          unreachable={state.kind === 'ready' ? state.unreachable : []}
          uploads={uploads}
          onOpenProject={(id, machine) =>
            onNavigate({ project: id, board: null, full: false, ...(machine ? { machine } : {}) })
          }
          onRefresh={refresh}
        />
      </div>
    );
  }

  if (!projects) {
    if (location.full) {
      return (
        <div className="tw-dhome tw-dhome-full">
          <div className="tw-dhome-board-stage">
            <div
              className="tw-preview-frame tw-skeleton"
              role="status"
              aria-label={t('design.libraryHome.openingBoard')}
            />
          </div>
        </div>
      );
    }
    return (
      <div className="tw-dhome">
        <div className="tw-dhome-scroll">
          <header className="tw-dhome-head">
            <BackLink label="Claude Design" onClick={toProjects} />
          </header>
          <GalleryGrid label={t('design.libraryHome.loadingBoards')} loading />
        </div>
      </div>
    );
  }

  const project = findProject(projects, location.project, location.machine);
  if (!project) {
    return (
      <div className="tw-dhome">
        <div className="tw-dhome-scroll">
          <header className="tw-dhome-head">
            <BackLink label="Claude Design" onClick={toProjects} />
          </header>
          <LibraryNotFound what={t('design.libraryHome.projectDeleted')} onProjects={toProjects} />
        </div>
      </div>
    );
  }

  const where = project.machineId ? { machine: project.machineId } : {};
  const toProject = () => onNavigate({ project: project.id, board: null, full: false, ...where });
  const toBoard = (board: string) => onNavigate({ project: project.id, board, full: false, ...where });
  const boardPath = location.board;
  if (boardPath !== null && location.full) {
    return (
      <FullBoard
        key={`${project.machineId ?? ''}:${project.id}`}
        project={project}
        boardPath={boardPath}
        onClose={() => toBoard(boardPath)}
        onOpenProjects={toProjects}
      />
    );
  }
  return (
    <div className="tw-dhome">
      {boardPath === null ? (
        <ProjectPage
          key={`${project.machineId ?? ''}:${project.id}`}
          project={project}
          onBack={toProjects}
          onOpenBoard={toBoard}
        />
      ) : (
        <BoardPage
          key={`${project.machineId ?? ''}:${project.id}`}
          project={project}
          boardPath={boardPath}
          onOpenProject={toProject}
          onOpenProjects={toProjects}
          onOpenBoard={toBoard}
          onOpenFull={() => onNavigate({ project: project.id, board: boardPath, full: true, ...where })}
        />
      )}
    </div>
  );
};

export default LibraryHome;
