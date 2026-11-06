// The mode of a project, at the head of its bar beside the address: two parts side by side, «Просмотр» | «Редактирование».
// The part of the mode the screen is in is lit; a click on the other goes there (or, where editing is not allowed, asks
// for access). Without a way to the other mode it is one quiet label. The site's bar, a deck's, a video's and an app
// document's draw the same switch.
import type { ISiteMode } from '@/next/features/designly/site-view';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/mode.css';

/** compact: a phone's bar, each part said by its icon only (the hint says the rest). */
const ModeButton = ({ mode, compact = false }: { mode: ISiteMode; compact?: boolean }) => {
  const { action } = mode;
  if (!action)
    return (
      <span className="hx-mode" data-editing={mode.editing} data-compact={compact} title={mode.state}>
        <Icon name={mode.editing ? 'pencil' : 'eye'} size="sm" className="hx-mode__icon" />
        <span className="hx-mode__name">{mode.state}</span>
      </span>
    );
  const ask = !mode.editing && action.ask;
  const parts = [
    {
      k: 'view',
      on: !mode.editing,
      icon: 'eye',
      label: t('next.site.modeView'),
      tip: t('next.site.modeView'),
    },
    {
      k: 'edit',
      on: mode.editing,
      icon: ask ? 'lock' : 'pencil',
      label: t('next.site.stateEditShort'),
      tip: ask ? t('next.site.modeAsk') : t('next.site.stateEditShort'),
    },
  ] as const;
  return (
    <div
      className="hx-modeseg"
      role="group"
      aria-label={t('next.site.modeSwitch')}
      data-editing={mode.editing}
      data-compact={compact}
    >
      <span className="hx-modeseg__thumb" aria-hidden="true" />
      {parts.map((part) => (
        <button
          key={part.k}
          type="button"
          className="hx-modeseg__part hx-tipped"
          data-k={`mode-${part.k}`}
          data-ask={part.k === 'edit' && ask ? true : undefined}
          aria-pressed={part.on}
          aria-label={part.tip}
          onClick={part.on ? undefined : action.onClick}
        >
          <Icon name={part.icon} size="sm" className="hx-modeseg__icon" />
          <span className="hx-modeseg__name">{part.label}</span>
          <span className="hx-tip" data-pos="below" data-align={part.k === 'view' ? 'start' : 'end'}>
            {part.tip}
          </span>
        </button>
      ))}
    </div>
  );
};

export default ModeButton;
