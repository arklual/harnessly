import { useEffect, useId, useRef } from 'react';
import { ChevronRight, MoreHorizontal, Plus } from 'lucide-react';
import { LAYER_ALIGN, TwMenuButton, TwSpinner, cx, useTwContextMenu } from '@/components/agents/ui';
import ConversationRow, { STATUS_LABEL } from '@/components/agents/sidebar/conversation-row';
import ProjectTile from '@/components/agents/sidebar/project-tile';
import { newConversationMenuItems, projectMenuItems } from '@/components/agents/sidebar/menus';
import { useSidebarContext } from '@/components/agents/sidebar/sidebar-context';
import useSidebarView from '@/hooks/use-sidebar-view';
import { PROJECT_PAGE, type IProjectModel } from '@/lib/hl-sidebar-model';
import { t } from '@/lib/i18n';

/** One project: a folder that opens in place, its newest five conversations and «Show more». */
const ProjectFolder = ({ model }: { model: IProjectModel }) => {
  const context = useSidebarContext();
  const { project, rows, shown, collapsed, summary } = model;
  const toggleProject = useSidebarView((s) => s.toggleProject);
  const toggleList = useSidebarView((s) => s.toggleList);
  const reveal = useSidebarView((s) => (s.reveal?.projectId === project.id ? s.reveal.nonce : null));
  const folderRef = useRef<HTMLDivElement>(null);
  const bodyId = useId();
  const menuItems = projectMenuItems(model, context);
  const contextMenu = useTwContextMenu({
    items: menuItems,
    'aria-label': t('sidebar.projectFolder.project', { name: project.name }),
  });
  const pending = context.pendingKeys.has(project.id);
  const hidden = rows.length - shown;

  useEffect(() => {
    const element = folderRef.current;
    if (reveal === null || !element) return;
    element.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    element.classList.remove('tw-flash');
    void element.offsetWidth;
    element.classList.add('tw-flash');
  }, [reveal]);

  return (
    <div ref={folderRef} className="tw-sfolder" data-project-id={project.id}>
      <div
        {...contextMenu.bind}
        className={cx('tw-row tw-srow tw-sfolder-head', (contextMenu.open || pending) && 'tw-srow-held')}
      >
        <button
          type="button"
          className="tw-srow-main"
          aria-expanded={!collapsed}
          aria-controls={bodyId}
          onClick={() => toggleProject(project.id)}
        >
          <span className="tw-srow-lead tw-sfolder-lead">
            <ProjectTile project={project} />
            <ChevronRight className="tw-sfolder-chevron" aria-hidden="true" />
          </span>
          <span className="tw-row-title">{project.name}</span>
          {summary && (
            <span
              className={cx('tw-sfolder-summary', `tw-sfolder-summary-${summary.status}`)}
              title={`${STATUS_LABEL[summary.status]}: ${summary.count}`}
            >
              <span className={cx('tw-row-dot', `tw-row-dot-${summary.status}`)} aria-hidden="true" />
              {summary.count}
            </span>
          )}
          {!summary && rows.length > 0 && <span className="tw-sfolder-count">{rows.length}</span>}
        </button>
        <span className="tw-srow-actions">
          {pending ? (
            <span className="tw-srow-spinner">
              <TwSpinner label={t('sidebar.projectFolder.saving')} />
            </span>
          ) : (
            <>
              <TwMenuButton
                items={newConversationMenuItems(context, project.id)}
                menuLabel={t('sidebar.projectFolder.newInProjectNamed', { name: project.name })}
                align={LAYER_ALIGN.end}
                className="tw-srow-more"
                aria-label={t('sidebar.projectFolder.newInProjectNamed', { name: project.name })}
                title={t('sidebar.projectFolder.newInProject')}
                disabled={context.busy}
              >
                <Plus />
              </TwMenuButton>
              <TwMenuButton
                items={menuItems}
                menuLabel={t('sidebar.projectFolder.project', { name: project.name })}
                align={LAYER_ALIGN.end}
                className="tw-srow-more"
                aria-label={t('sidebar.projectFolder.projectActionsNamed', { name: project.name })}
                title={t('sidebar.projectFolder.projectActions')}
              >
                <MoreHorizontal />
              </TwMenuButton>
            </>
          )}
        </span>
        {contextMenu.menu}
      </div>
      {!collapsed && (
        <div id={bodyId} className="tw-sfolder-body">
          {rows.slice(0, shown).map((row) => (
            <ConversationRow key={row.ref} row={row} nested />
          ))}
          {rows.length === 0 && (
            <p className="tw-sfolder-empty" title={t('sidebar.projectFolder.emptyHint')}>
              {t('sidebar.projectFolder.empty')}
            </p>
          )}
          {hidden > 0 && (
            <button type="button" className="tw-more tw-sfolder-more" onClick={() => toggleList(project.id)}>
              {t('sidebar.projectFolder.showMore', { count: hidden })}
            </button>
          )}
          {hidden === 0 && shown > PROJECT_PAGE && (
            <button type="button" className="tw-more tw-sfolder-more" onClick={() => toggleList(project.id)}>
              {t('sidebar.projectFolder.collapse')}
            </button>
          )}
        </div>
      )}
    </div>
  );
};

export default ProjectFolder;
