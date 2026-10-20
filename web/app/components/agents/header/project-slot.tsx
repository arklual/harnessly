import type { IHeaderSlotProps } from '@/components/agents/header/header-slot';
import ProjectTile from '@/components/agents/sidebar/project-tile';
import { organizerActions } from '@/components/agents/sidebar/organizer-actions';
import useHlOrganizer from '@/hooks/use-hl-organizer';
import useSidebarView from '@/hooks/use-sidebar-view';
import { ORGANIZER_LIMITS, isConversationRef } from '@/lib/hl-organizer';
import { t } from '@/lib/i18n';

export interface IPinState {
  pinned: boolean;
  pending: boolean;
  /** Why pinning is unavailable, e.g. the pin limit is reached. */
  blockedReason: string | null;
  toggle: () => void;
}

/** Pin toggle of the conversation on screen; null when it cannot be pinned. */
export const usePinState = ({ conversation }: IHeaderSlotProps): IPinState | null => {
  const { organizer, pendingKeys } = useHlOrganizer();
  const ref = conversation && isConversationRef(conversation.key) ? conversation.key : null;
  if (!ref || !organizer) return null;
  const pinned = organizer.pins.includes(ref);
  const pending = pendingKeys.has(ref);
  const full = !pinned && organizer.pins.length >= ORGANIZER_LIMITS.pins;
  return {
    pinned,
    pending,
    blockedReason: full ? t('header.projectSlot.pinLimit', { n: ORGANIZER_LIMITS.pins }) : null,
    toggle: () => {
      if (pending || full) return;
      void (pinned ? organizerActions.unpin(ref) : organizerActions.pin(ref));
    },
  };
};

/** Project crumb of the conversation on screen: opens the project in the panel. */
const ProjectSlot = ({ conversation }: IHeaderSlotProps) => {
  const { organizer } = useHlOrganizer();
  const revealProject = useSidebarView((s) => s.revealProject);
  const ref = conversation && isConversationRef(conversation.key) ? conversation.key : null;
  if (!ref || !organizer) return null;
  const project = organizer.projects.find((p) => p.id === organizer.assignments[ref]);
  if (!project) return null;
  return (
    <>
      <span className="tw-crumb-sep" aria-hidden="true">
        /
      </span>
      <button
        type="button"
        className="tw-crumb tw-crumb-project"
        title={t('header.projectSlot.openProject', { name: project.name })}
        onClick={() => revealProject(project.id)}
      >
        <ProjectTile project={project} size="sm" />
        <span>{project.name}</span>
      </button>
    </>
  );
};

export default ProjectSlot;
