import { useRef, useState, type DragEvent } from 'react';
import { FolderOpen, Plus, Share2, Upload } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TW_MENU_KIND, TwButton, TwTooltip } from '@/components/agents/ui';
import { GalleryCard, GalleryGrid, Note } from '@/components/agents/design/library/library-parts';
import { NewProjectDialog } from '@/components/agents/design/library/new-project-dialog';
import useMinuteClock from '@/components/agents/sidebar/use-minute-clock';
import useDesignLink from '@/hooks/use-design-link';
import { UPLOAD_STATUS, type IUploadItem } from '@/hooks/use-design-uploads';
import { scopedAppPath } from '@/lib/app-path';
import { spansMachines, type ILibraryProject } from '@/lib/hl-design-library/fleet';
import { mainBoard } from '@/lib/hl-design-library/group';
import { projectCaption, titleInitials } from '@/lib/hl-design-library/home-model';
import { libraryThumbPath } from '@/lib/hl-design-library/types';
import { openConversation } from '@/lib/hl-open-conversation';
import { plural, t } from '@/lib/i18n';

const UPLOAD_LABEL = {
  [UPLOAD_STATUS.waiting]: t('design.projectsGrid.statusWaiting'),
  [UPLOAD_STATUS.sending]: t('design.projectsGrid.statusSending'),
  [UPLOAD_STATUS.unpacking]: t('design.projectsGrid.statusUnpacking'),
  [UPLOAD_STATUS.done]: t('design.projectsGrid.statusDone'),
  [UPLOAD_STATUS.error]: t('design.projectsGrid.statusError'),
} as const;

interface IUploadListProps {
  items: readonly IUploadItem[];
  busy: boolean;
  onDismiss: () => void;
}

const UploadList = ({ items, busy, onDismiss }: IUploadListProps) => (
  <section className="tw-dhome-uploads" aria-label={t('design.projectsGrid.uploads')}>
    <ul>
      {items.map((item) => (
        <li key={item.id} className="tw-dhome-upload" data-status={item.status}>
          <span className="tw-dhome-upload-name" title={item.name}>
            {item.name}
          </span>
          <span className="tw-dhome-upload-state">{item.message ?? UPLOAD_LABEL[item.status]}</span>
          {(item.status === UPLOAD_STATUS.sending || item.status === UPLOAD_STATUS.unpacking) && (
            <progress
              className="tw-dhome-upload-bar"
              max={1}
              value={item.status === UPLOAD_STATUS.unpacking ? undefined : item.progress}
              aria-label={`${item.name}: ${UPLOAD_LABEL[item.status]}`}
            />
          )}
        </li>
      ))}
    </ul>
    {!busy && (
      <button type="button" className="tw-dhome-quiet" onClick={onDismiss}>
        {t('design.projectsGrid.hide')}
      </button>
    )}
  </section>
);

const coverOf = (project: ILibraryProject): string | null => {
  const board = mainBoard(project);
  return board
    ? scopedAppPath(libraryThumbPath(project.id, board.path, project.updatedAt), project.machineId)
    : null;
};

interface IProjectsGridProps {
  /** null while the library is loading, [] when it is empty. */
  projects: readonly ILibraryProject[] | null;
  /** Machines that did not answer: their projects may be missing or out of date. */
  unreachable: readonly string[];
  uploads: {
    items: readonly IUploadItem[];
    busy: boolean;
    add: (files: readonly File[]) => void;
    dismiss: () => void;
  };
  onOpenProject: (id: string, machine?: string) => void;
  onRefresh: () => Promise<void>;
}

/** Level one: the projects of the shared library as covers with names. Zips dropped anywhere on it become projects. */
export const ProjectsGrid = ({
  projects,
  unreachable,
  uploads,
  onOpenProject,
  onRefresh,
}: IProjectsGridProps) => {
  const [creating, setCreating] = useState(false);
  const [dragging, setDragging] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const now = useMinuteClock();
  const { share } = useDesignLink();
  const withMachines = projects !== null && spansMachines(projects);

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
    const files = Array.from(event.dataTransfer.files);
    if (files.length > 0) uploads.add(files);
  };

  return (
    <div
      className="tw-dhome-dropzone"
      data-dragging={dragging || undefined}
      onDragOver={onDragOver}
      onDragLeave={(event) => {
        if (event.currentTarget === event.target) setDragging(false);
      }}
      onDrop={onDrop}
    >
      <div className="tw-dhome-scroll">
        <header className="tw-dhome-head tw-dhome-head-inline">
          <h1 className="tw-dhome-title">Claude Design</h1>
          <div className="tw-dhome-actions">
            <TwTooltip content={t('design.projectsGrid.uploadZip')}>
              <TwButton
                size={BUTTON_SIZE.md}
                variant={BUTTON_VARIANT.ghost}
                icon={<Upload />}
                pending={uploads.busy}
                aria-label={t('design.projectsGrid.uploadZip')}
                onClick={() => input.current?.click()}
              />
            </TwTooltip>
            <TwButton
              size={BUTTON_SIZE.md}
              variant={BUTTON_VARIANT.primary}
              icon={<Plus />}
              onClick={() => setCreating(true)}
            >
              {t('design.projectsGrid.newProject')}
            </TwButton>
          </div>
          <input
            ref={input}
            type="file"
            accept=".zip,application/zip"
            multiple
            className="tw-dlib-file"
            tabIndex={-1}
            aria-hidden="true"
            onChange={(event) => {
              const files = Array.from(event.target.files ?? []);
              event.target.value = '';
              if (files.length > 0) uploads.add(files);
            }}
          />
        </header>

        {uploads.items.length > 0 && (
          <UploadList items={uploads.items} busy={uploads.busy} onDismiss={uploads.dismiss} />
        )}

        {unreachable.length > 0 && (
          <Note>
            {plural(unreachable.length, 'design.projectsGrid.unreachable', {
              machines: unreachable.join(', '),
            })}
          </Note>
        )}
        {projects === null && <GalleryGrid label={t('design.projectsGrid.loading')} loading />}
        {projects !== null && projects.length === 0 && (
          <div className="tw-dhome-empty">
            <FolderOpen aria-hidden="true" />
            <strong>{t('design.projectsGrid.empty')}</strong>
            <p>{t('design.projectsGrid.emptyHint')}</p>
          </div>
        )}
        {projects !== null && projects.length > 0 && (
          <GalleryGrid label={t('design.projectsGrid.projects')}>
            {projects.map((project) => (
              <li key={`${project.machineId ?? ''}:${project.id}`}>
                <GalleryCard
                  title={project.title}
                  caption={
                    withMachines && project.machineName
                      ? `${projectCaption(project, now)} · ${project.machineName}`
                      : projectCaption(project, now)
                  }
                  src={coverOf(project)}
                  fallback={titleInitials(project.title)}
                  onOpen={() => onOpenProject(project.id, project.machineId)}
                  menu={[
                    {
                      kind: TW_MENU_KIND.action,
                      id: 'share',
                      label: t('design.projectsGrid.share'),
                      icon: <Share2 />,
                      onSelect: () =>
                        void share(
                          {
                            project: project.id,
                            board: null,
                            full: false,
                            ...(project.machineId ? { machine: project.machineId } : {}),
                          },
                          project.title,
                        ),
                    },
                  ]}
                />
              </li>
            ))}
          </GalleryGrid>
        )}

        <NewProjectDialog
          open={creating}
          onClose={() => setCreating(false)}
          onCreated={({ tabId }) => {
            setCreating(false);
            void onRefresh();
            openConversation(tabId);
          }}
          onCreatedWithoutChat={(id) => {
            setCreating(false);
            void onRefresh();
            onOpenProject(id);
          }}
        />
      </div>
      {dragging && (
        <div className="tw-dhome-drop" aria-hidden="true">
          {t('design.projectsGrid.drop')}
        </div>
      )}
    </div>
  );
};
