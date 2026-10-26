// One preset of a vendor on the page: its key panel, fed by what the runtime lists of the preset.
import KeyPanel from '@/next/features/providers/key-panel';
import type { IPresetActions, IPresetState } from '@/next/features/providers/model';
import { t } from '@/next/i18n';

interface IPresetPanelProps extends IPresetActions {
  state: IPresetState;
  /** The vendor's name, for the field's label. */
  vendor: string;
  onSaved?: () => void;
}

const PresetPanel = ({ state, vendor, onSave, onRemove, onTest, onSaved }: IPresetPanelProps) => (
  <KeyPanel
    hint={state.hint}
    label={t('next.providers.keyAria', { vendor })}
    saved={state.key}
    onSave={(key) => onSave(state.id, key)}
    onRemove={() => onRemove(state.id)}
    onTest={() => onTest(state.id)}
    onSaved={onSaved}
  />
);

export default PresetPanel;
