// The mode of a project, at the head of its bar beside the address: «Макет» while looking, «Редактирование макета»
// while editing; a click goes to the other mode (or asks for access), the hint says which. The site's bar, a deck's,
// a video's and an app document's draw the same switch.
import type { ISiteMode } from '@/next/features/designly/site-view';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/mode.css';

/** compact: a phone's bar, the mode said by its icon only. */
const ModeButton = ({ mode, compact = false }: { mode: ISiteMode; compact?: boolean }) => {
  const { action } = mode;
  const body = (
    <>
      <Icon name={mode.editing ? 'pencil' : 'eye'} size="sm" className="hx-mode__icon" />
      <span className="hx-mode__name">{mode.state}</span>
      {/* Editing, a bar of usual width says it shorter: the project's name beside it keeps its room. */}
      {mode.editing && (
        <span className="hx-mode__short" aria-hidden="true">
          {t('next.site.stateEditShort')}
        </span>
      )}
    </>
  );
  if (!action)
    return (
      <span className="hx-mode" data-editing={mode.editing} data-compact={compact} title={mode.state}>
        {body}
      </span>
    );
  return (
    <button
      type="button"
      className="hx-mode hx-tipped"
      data-k="mode"
      data-editing={mode.editing}
      data-compact={compact}
      aria-label={`${mode.state}: ${action.label}`}
      onClick={action.onClick}
    >
      {body}
      <span className="hx-tip" data-pos="below" data-align="start">
        {action.label}
      </span>
    </button>
  );
};

export default ModeButton;
