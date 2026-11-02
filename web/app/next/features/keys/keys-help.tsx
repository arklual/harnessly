// The shortcuts sheet (⌘/ or ?): what the keyboard does on the screen now — the open deck, video or picture first,
// then everywhere — with each action's shortcuts as this keyboard writes them, and a way to Settings → «Клавиши».
import Modal from '@/next/features/shell/modal';
import { chordLabel, isMac } from '@/next/features/keys/chord';
import { KEY_COMMANDS, KEY_SCOPE, type TKeyScope } from '@/next/features/keys/commands';
import { keysOf, type TKeyOverrides } from '@/next/features/keys/keymap';
import { useKeyOverrides } from '@/next/features/keys/use-keys';
import { t } from '@/next/i18n';
import '@/next/styles/keys.css';

/** A shortcut as key caps: ⇧⌘O, or Ctrl+Shift+O. */
export const KeyCaps = ({ chord }: { chord: string }) => (
  <kbd className="hx-keycap">{chordLabel(chord, t('next.keys.space'), isMac())}</kbd>
);

/** An action's first shortcut as this keyboard writes it, for a hint beside its button; null when it has none. */
export const useKeyHint = (id: string): string | null => {
  const chord = keysOf(id, useKeyOverrides())[0];
  return chord === undefined ? null : chordLabel(chord, t('next.keys.space'), isMac());
};

/** The screens whose actions the sheet lists: the open ones, the latest first, then the global ones. */
export const helpScopes = (open: readonly TKeyScope[]): readonly TKeyScope[] => {
  const shown = [...new Set([...open].reverse())].filter((s) => s !== KEY_SCOPE.global);
  return [...shown, KEY_SCOPE.global];
};

interface IKeysHelpProps {
  open: boolean;
  onClose: () => void;
  /** The screens with keys when the sheet opened: its own opening adds none. */
  scopes: readonly TKeyScope[];
  overrides: TKeyOverrides;
  onSettings: () => void;
}

const KeysHelp = ({ open, onClose, scopes, overrides, onSettings }: IKeysHelpProps) => (
  <Modal open={open} onClose={onClose} title={t('next.keys.title')} boxClass="hx-keys-help">
    <div className="hx-modal__body">
      {helpScopes(scopes).map((scope) => {
        const rows = KEY_COMMANDS.filter((c) => c.scope === scope && keysOf(c.id, overrides).length > 0);
        return (
          <section key={scope} className="hx-keys-help__group">
            <h3 className="hx-keys-help__title">{t(`next.keys.scope.${scope}`)}</h3>
            {rows.map((c) => (
              <div key={c.id} className="hx-keys-help__row">
                <span>{t(`next.keys.cmd.${c.id}`)}</span>
                <span className="hx-keys-help__caps">
                  {keysOf(c.id, overrides).map((chord) => (
                    <KeyCaps key={chord} chord={chord} />
                  ))}
                </span>
              </div>
            ))}
          </section>
        );
      })}
    </div>
    <div className="hx-modal__foot">
      <span className="hx-spacer" />
      <button type="button" className="hx-btn" data-variant="ghost" onClick={onSettings}>
        {t('next.keys.change')}
      </button>
    </div>
  </Modal>
);

export default KeysHelp;
