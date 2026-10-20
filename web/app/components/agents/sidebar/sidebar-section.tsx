import { useId, type ReactNode } from 'react';
import { ChevronDown } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import useSidebarView from '@/hooks/use-sidebar-view';
import type { TSidebarSection } from '@/lib/hl-sidebar-model';

interface ISidebarSectionProps {
  section: TSidebarSection;
  title: string;
  /** Buttons on the right of the heading (new project, …). */
  actions?: ReactNode;
  children: ReactNode;
}

/** A foldable block of the panel: «Pinned», «Projects», «Conversations». Folding is per device. */
const SidebarSection = ({ section, title, actions, children }: ISidebarSectionProps) => {
  const collapsed = useSidebarView((s) => s.view.collapsedSections.includes(section));
  const toggleSection = useSidebarView((s) => s.toggleSection);
  const bodyId = useId();
  return (
    <section className={cx('tw-ssection', `tw-ssection-${section}`)} aria-label={title}>
      <div className="tw-ssection-head">
        <button
          type="button"
          className="tw-ssection-toggle"
          aria-expanded={!collapsed}
          aria-controls={bodyId}
          onClick={() => toggleSection(section)}
        >
          <span>{title}</span>
          <ChevronDown className="tw-ssection-chevron" aria-hidden="true" />
        </button>
        {actions && <span className="tw-ssection-actions">{actions}</span>}
      </div>
      {!collapsed && (
        <div id={bodyId} className="tw-ssection-body">
          {children}
        </div>
      )}
    </section>
  );
};

export default SidebarSection;
