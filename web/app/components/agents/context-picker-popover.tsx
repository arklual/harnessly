import { TwKbd, TwListbox } from '@/components/agents/ui';
import type { IUseContextPicker } from '@/hooks/use-context-picker';
import { t } from '@/lib/i18n';

interface IContextPickerPopoverProps {
  picker: IUseContextPicker;
}

/**
 * The «@»-context list: reuses TwListbox exactly as the slash-command popover does
 * (see slash-command-popover.tsx), with a group name as a disabled, unselectable row above its items.
 */
const ContextPickerPopover = ({ picker }: IContextPickerPopoverProps) => {
  if (!picker.open) return null;
  return (
    <div className="tw-at-popover" onMouseDown={(event) => event.preventDefault()}>
      <div className="tw-at-backdrop" onClick={picker.close} aria-hidden="true" />
      <div className="tw-at-sheet">
        <div className="tw-at-head">
          <span className="tw-at-title" id={`${picker.listId}-title`}>
            {t('agents.contextPickerPopover.title')}
          </span>
          <span className="tw-at-keys" aria-hidden="true">
            <TwKbd>↑↓</TwKbd> {t('agents.contextPickerPopover.pick')} · <TwKbd>Enter</TwKbd> ·{' '}
            <TwKbd>Esc</TwKbd>
          </span>
        </div>
        <TwListbox
          id={picker.listId}
          options={picker.options}
          value={null}
          aria-labelledby={`${picker.listId}-title`}
          activeValue={picker.activeValue}
          onActiveValueChange={picker.setActiveValue}
          focusable={false}
          emptyText={t('agents.contextPickerPopover.empty')}
          loadingText={t('agents.contextPickerPopover.loading')}
          className="tw-at-list"
          onChange={(value) => picker.pick(value)}
        />
      </div>
    </div>
  );
};

export default ContextPickerPopover;
