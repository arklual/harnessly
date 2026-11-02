// The person's shortcuts: what they changed (PersonSettings.keys, an action's list of shortcuts; an empty list turns it
// off) over the list's own. Kept with the account, so every device of the person has the same.
import { KEY_COMMANDS, commandOf, type IKeyCommand } from '@/next/features/keys/commands';

export type TKeyOverrides = Readonly<Record<string, readonly string[]>>;

/** An action's shortcuts now. */
export const keysOf = (id: string, overrides: TKeyOverrides): readonly string[] =>
  Object.hasOwn(overrides, id) ? overrides[id] : (commandOf(id)?.keys ?? []);

/** Whether an action has the shortcuts it came with. */
export const isDefault = (id: string, overrides: TKeyOverrides): boolean => !Object.hasOwn(overrides, id);

/** The actions a shortcut runs, of every screen. */
export const commandsOfChord = (chord: string, overrides: TKeyOverrides): readonly IKeyCommand[] =>
  KEY_COMMANDS.filter((c) => keysOf(c.id, overrides).includes(chord));

/** The action of the same screen a shortcut belongs to already, other than `id`; null when it is free there. */
export const takenBy = (id: string, chord: string, overrides: TKeyOverrides): IKeyCommand | null => {
  const own = commandOf(id);
  if (!own) return null;
  return commandsOfChord(chord, overrides).find((c) => c.id !== id && c.scope === own.scope) ?? null;
};

/** The overrides kept as they differ from the list: an action back to its own shortcuts leaves the record. */
const settled = (overrides: TKeyOverrides, id: string, keys: readonly string[]): TKeyOverrides => {
  const own = commandOf(id)?.keys ?? [];
  const next = { ...overrides };
  if (keys.length === own.length && keys.every((k, i) => k === own[i])) delete next[id];
  else next[id] = keys;
  return next;
};

/**
 * Gives `id` the shortcut in place of `old` (or as one more when `old` is null); the action of the same screen that had
 * it lets it go.
 */
export const withChord = (
  overrides: TKeyOverrides,
  id: string,
  chord: string,
  old: string | null,
): TKeyOverrides => {
  let next = overrides;
  const other = takenBy(id, chord, next);
  if (other?.fixed) return overrides;
  if (other)
    next = settled(
      next,
      other.id,
      keysOf(other.id, next).filter((k) => k !== chord),
    );
  const mine = keysOf(id, next).filter((k) => k !== chord);
  const at = old === null ? -1 : mine.indexOf(old);
  const keys = at < 0 ? [...mine, chord] : mine.map((k, i) => (i === at ? chord : k));
  return settled(next, id, keys);
};

/** Takes one shortcut from an action. */
export const withoutChord = (overrides: TKeyOverrides, id: string, chord: string): TKeyOverrides =>
  settled(
    overrides,
    id,
    keysOf(id, overrides).filter((k) => k !== chord),
  );

/** The action back to its own shortcuts. */
export const reset = (overrides: TKeyOverrides, id: string): TKeyOverrides => {
  const next = { ...overrides };
  delete next[id];
  return next;
};
