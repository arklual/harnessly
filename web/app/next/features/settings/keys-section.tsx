// Settings → «Клавиши»: every action of the keyboard by its screen, with its shortcuts. A press on a shortcut waits for
// the new one (Esc leaves it, ⌫ takes it away), «+» adds one more, the arrow puts the action's own back. A shortcut
// another action of the same screen had moves here and the page says from where; one the page keeps (Esc, the digits)
// is not given. Saved with the account: every device of the person has the same.
import { useEffect, useEffectEvent, useState } from 'react';
import { chordOf } from '@/next/features/keys/chord';
import { KEY_COMMANDS, KEY_SCOPES, type IKeyCommand } from '@/next/features/keys/commands';
import { KeyCaps } from '@/next/features/keys/keys-help';
import { isDefault, keysOf, takenBy, type TKeyOverrides } from '@/next/features/keys/keymap';
import { Group, Spin } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import '@/next/styles/keys.css';

/** What is being recorded: an action's shortcut to replace, or a new one (`old` null). */
interface IRecording {
  id: string;
  old: string | null;
}

export const KEY_CHANGE = { set: 'set', drop: 'drop', reset: 'reset' } as const;
export type TKeyChange =
  | { k: typeof KEY_CHANGE.set; id: string; chord: string; old: string | null }
  | { k: typeof KEY_CHANGE.drop; id: string; chord: string }
  | { k: typeof KEY_CHANGE.reset; id: string | null };

interface IKeysSectionProps {
  overrides: TKeyOverrides;
  /** The actions whose change waits for the server. */
  busy: ReadonlySet<string>;
  onChange: (change: TKeyChange) => void;
  /** Tells the person why a shortcut was not given (the page's own key). */
  onRefused: (chord: string, by: IKeyCommand) => void;
}

const MODIFIER_ONLY = new Set(['Shift', 'Control', 'Alt', 'Meta']);

interface IRowProps {
  command: IKeyCommand;
  overrides: TKeyOverrides;
  busy: boolean;
  recording: IRecording | null;
  onRecord: (next: IRecording | null) => void;
  onChange: (change: TKeyChange) => void;
}

const KeyRow = ({ command, overrides, busy, recording, onRecord, onChange }: IRowProps) => {
  const keys = keysOf(command.id, overrides);
  const own = recording?.id === command.id ? recording : null;

  const waiting = <span className="hx-keys-set__wait">{t('next.keys.press')}</span>;
  return (
    <div className="hx-set__row hx-keys-set__row" data-busy={busy || undefined} aria-busy={busy || undefined}>
      <div className="hx-set__row-text">
        <span className="hx-set__row-label">{t(`next.keys.cmd.${command.id}`)}</span>
      </div>
      <Spin />
      <div className="hx-keys-set__keys">
        {keys.length === 0 && !own && <span className="hx-keys-set__off">{t('next.keys.off')}</span>}
        {keys.map((chord) => (
          <button
            key={chord}
            type="button"
            className="hx-keys-set__key"
            data-on={own?.old === chord || undefined}
            aria-label={t('next.keys.changeOne')}
            disabled={busy}
            onClick={() => onRecord(own?.old === chord ? null : { id: command.id, old: chord })}
          >
            {own?.old === chord ? waiting : <KeyCaps chord={chord} />}
          </button>
        ))}
        {own?.old === null && (
          <button type="button" className="hx-keys-set__key" data-on onClick={() => onRecord(null)}>
            {waiting}
          </button>
        )}
        <button
          type="button"
          className="hx-keys-set__act"
          aria-label={t('next.keys.add')}
          title={t('next.keys.add')}
          disabled={busy}
          onClick={() => onRecord({ id: command.id, old: null })}
        >
          <Icon name="plus" size="sm" />
        </button>
        <button
          type="button"
          className="hx-keys-set__act"
          aria-label={t('next.keys.reset')}
          title={t('next.keys.reset')}
          data-hidden={isDefault(command.id, overrides) || undefined}
          disabled={busy || isDefault(command.id, overrides)}
          onClick={() => onChange({ k: KEY_CHANGE.reset, id: command.id })}
        >
          <Icon name="rotate-ccw" size="sm" />
        </button>
      </div>
    </div>
  );
};

const KeysSection = ({ overrides, busy, onChange, onRefused }: IKeysSectionProps) => {
  const [recording, setRecording] = useState<IRecording | null>(null);
  const press = useEffectEvent((event: globalThis.KeyboardEvent) => {
    if (recording === null || event.key === 'Tab' || MODIFIER_ONLY.has(event.key)) return;
    event.preventDefault();
    event.stopPropagation();
    const plain = !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey;
    if (plain && event.key === 'Escape') {
      setRecording(null);
      return;
    }
    if (plain && (event.key === 'Backspace' || event.key === 'Delete')) {
      if (recording.old) onChange({ k: KEY_CHANGE.drop, id: recording.id, chord: recording.old });
      setRecording(null);
      return;
    }
    const chord = chordOf(event);
    if (chord === null) return;
    const other = takenBy(recording.id, chord, overrides);
    if (other?.fixed) {
      onRefused(chord, other);
      return;
    }
    setRecording(null);
    if (chord !== recording.old) onChange({ k: KEY_CHANGE.set, id: recording.id, chord, old: recording.old });
  });
  // While a shortcut is recorded every key comes here first, before the page's own shortcuts; a press elsewhere stops.
  const on = recording !== null;
  useEffect(() => {
    if (!on) return;
    const down = (event: globalThis.KeyboardEvent) => press(event);
    const away = (event: PointerEvent) => {
      if (!(event.target instanceof Element) || !event.target.closest('.hx-keys-set__key[data-on]'))
        setRecording(null);
    };
    window.addEventListener('keydown', down, true);
    document.addEventListener('pointerdown', away, true);
    return () => {
      window.removeEventListener('keydown', down, true);
      document.removeEventListener('pointerdown', away, true);
    };
  }, [on]);
  const changed = Object.keys(overrides).length > 0;
  return (
    <>
      {KEY_SCOPES.map((scope) => (
        <Group key={scope} title={t(`next.keys.scope.${scope}`)}>
          {KEY_COMMANDS.filter((c) => c.scope === scope && !c.fixed).map((c) => (
            <KeyRow
              key={c.id}
              command={c}
              overrides={overrides}
              busy={busy.has(c.id)}
              recording={recording}
              onRecord={setRecording}
              onChange={onChange}
            />
          ))}
        </Group>
      ))}
      <div className="hx-keys-set__foot">
        <span className="hx-set__foot">{t('next.keys.foot')}</span>
        {changed && (
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            disabled={busy.size > 0}
            onClick={() => onChange({ k: KEY_CHANGE.reset, id: null })}
          >
            {t('next.keys.resetAll')}
          </button>
        )}
      </div>
    </>
  );
};

export default KeysSection;
