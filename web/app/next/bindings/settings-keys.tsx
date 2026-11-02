// Settings → «Клавиши»: the person's shortcuts, saved with the account (PersonSettings.keys) as each change is made;
// the page shows the change once the server has it.
import type { Pending } from '@/next/bindings/settings-agents';
import { chordLabel, isMac } from '@/next/features/keys/chord';
import type { IKeyCommand } from '@/next/features/keys/commands';
import { reset, takenBy, withChord, withoutChord, type TKeyOverrides } from '@/next/features/keys/keymap';
import { useKeyOverrides } from '@/next/features/keys/use-keys';
import KeysSection, { KEY_CHANGE, type TKeyChange } from '@/next/features/settings/keys-section';
import { t } from '@/next/i18n';
import { patchSettings } from '@/next/stores/me';
import { toast } from '@/next/ui/toast';

const label = (chord: string) => chordLabel(chord, t('next.keys.space'), isMac());

const save = async (keys: TKeyOverrides): Promise<void> => {
  const res = await patchSettings({ keys: keys as Record<string, string[]> });
  if (res.r === 'accepted') return;
  throw new Error(res.r === 'refused' ? t(`next.refuse.${res.code}`) : t('next.failure.network'));
};

/** The record after a change, and the action the shortcut moved from, if any. */
const applied = (
  overrides: TKeyOverrides,
  change: TKeyChange,
): { next: TKeyOverrides; from: IKeyCommand | null } => {
  if (change.k === KEY_CHANGE.set)
    return {
      next: withChord(overrides, change.id, change.chord, change.old),
      from: takenBy(change.id, change.chord, overrides),
    };
  if (change.k === KEY_CHANGE.drop)
    return { next: withoutChord(overrides, change.id, change.chord), from: null };
  return { next: change.id === null ? {} : reset(overrides, change.id), from: null };
};

export const KeysPage = ({ pending }: { pending: Pending }) => {
  const overrides = useKeyOverrides();
  const { run, pending: waiting } = pending;
  const onChange = (change: TKeyChange) => {
    const { next, from } = applied(overrides, change);
    const key = change.id ?? 'keys';
    void run(key, t('next.settings.failed'), async () => {
      await save(next);
      if (from && change.k === KEY_CHANGE.set)
        toast(t('next.keys.moved', { keys: label(change.chord), from: t(`next.keys.cmd.${from.id}`) }));
    });
  };
  return (
    <KeysSection
      overrides={overrides}
      busy={new Set(waiting.keys())}
      onChange={onChange}
      onRefused={(chord, by) =>
        toast(t('next.keys.taken', { keys: label(chord), by: t(`next.keys.cmd.${by.id}`) }), {
          tone: 'error',
        })
      }
    />
  );
};
