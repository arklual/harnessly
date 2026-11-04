// The mode of a project in its bar: what the screen is in, with the way to the other mode beside it. Looking: «Макет»
// and «Редактировать» (or «Запросить доступ»); editing: «Редактирование макета» and a quiet eye back to looking. The
// site's bar, a deck's, a video's and an app document's draw the same switch.
import '@/next/styles/mode.css';
import type { ISiteMode } from '@/next/features/designly/site-view';
import Icon from '@/next/ui/icon';

/** compact: a phone's bar, the mode said by its icon only. */
const ModeButton = ({ mode, compact = false }: { mode: ISiteMode; compact?: boolean }) => {
  const { action } = mode;
  return (
    <div className="hx-mode" data-editing={mode.editing} data-compact={compact} data-act={action !== null}>
      <span className="hx-mode__state" title={mode.state}>
        <Icon name={mode.editing ? 'pencil' : 'eye'} size="sm" className="hx-mode__icon" />
        <span className="hx-mode__name">{mode.state}</span>
      </span>
      {action && mode.editing && (
        <button
          type="button"
          className="hx-mode__act hx-tipped"
          data-k="mode"
          aria-label={action.label}
          onClick={action.onClick}
        >
          <Icon name={action.icon} size="sm" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {action.label}
          </span>
        </button>
      )}
      {action && !mode.editing && (
        <button
          type="button"
          className="hx-mode__act"
          data-k="mode"
          data-primary="true"
          onClick={action.onClick}
        >
          <Icon name={action.icon} size="sm" />
          <span>{action.label}</span>
        </button>
      )}
    </div>
  );
};

export default ModeButton;
