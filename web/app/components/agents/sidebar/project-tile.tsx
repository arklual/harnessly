import { cx } from '@/components/agents/ui';
import type { IOrganizerProject } from '@/lib/hl-organizer';

interface IProjectTileProps {
  project: Pick<IOrganizerProject, 'name' | 'color'>;
  size?: 'sm' | 'md';
  className?: string;
}

/** Colored square with the first letter: the project's only icon in the MVP. */
const ProjectTile = ({ project, size = 'md', className }: IProjectTileProps) => (
  <span
    className={cx(
      'tw-project-tile',
      `tw-project-tile-${project.color}`,
      size === 'sm' && 'tw-project-tile-sm',
      className,
    )}
    aria-hidden="true"
  >
    {Array.from(project.name.trim())[0]?.toUpperCase() ?? '·'}
  </span>
);

export default ProjectTile;
