import { FolderPlus, Plus } from 'lucide-react';
import ProjectFolder from '@/components/agents/sidebar/project-folder';
import SidebarSection from '@/components/agents/sidebar/sidebar-section';
import { useSidebarContext } from '@/components/agents/sidebar/sidebar-context';
import type { ISidebarModel } from '@/lib/hl-sidebar-model';
import { t } from '@/lib/i18n';

const ProjectsSection = ({ model }: { model: ISidebarModel }) => {
  const context = useSidebarContext();
  return (
    <SidebarSection
      section="projects"
      title={t('sidebar.projectsSection.title')}
      actions={
        <button
          type="button"
          className="tw-icon-button tw-ssection-action"
          aria-label={t('sidebar.projectsSection.newProject')}
          title={t('sidebar.projectsSection.newProject')}
          onClick={() => context.onNewProject()}
        >
          <FolderPlus />
        </button>
      }
    >
      {model.projects.map((project) => (
        <ProjectFolder key={project.project.id} model={project} />
      ))}
      {model.projects.length === 0 && (
        <button
          type="button"
          className="tw-row tw-sempty"
          onClick={() => context.onNewProject()}
          title={t('sidebar.projectsSection.emptyHint')}
        >
          <span className="tw-srow-lead">
            <Plus aria-hidden="true" />
          </span>
          <span className="tw-row-title">{t('sidebar.projectsSection.newProject')}</span>
        </button>
      )}
    </SidebarSection>
  );
};

export default ProjectsSection;
