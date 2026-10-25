// Under the start page's field: the places the window can open, a row of quiet tiles (a new tab starts here).
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

export interface IHomeTool {
  id: string;
  icon: string;
  label: string;
  /** `newTab`: opened with ⌘ or the middle button. */
  onOpen: (newTab: boolean) => void;
}

const HomeTools = ({ tools }: { tools: readonly IHomeTool[] }) => (
  <nav className="hx-hometools hx-enter" data-enter="up" data-eb="520" aria-label={t('next.tabs.tools')}>
    {tools.map((tool, i) => (
      <button
        key={tool.id}
        type="button"
        className="hx-hometool hx-enter"
        data-enter="up"
        data-eb="560"
        data-i={i}
        onClick={(event) => tool.onOpen(event.metaKey || event.ctrlKey)}
        onMouseDown={(event) => {
          if (event.button === 1) event.preventDefault();
        }}
        onAuxClick={(event) => {
          if (event.button !== 1) return;
          event.preventDefault();
          tool.onOpen(true);
        }}
      >
        <span className="hx-hometool__ic">
          <Icon name={tool.icon} />
        </span>
        <span className="hx-hometool__name">{tool.label}</span>
      </button>
    ))}
  </nav>
);

export default HomeTools;
