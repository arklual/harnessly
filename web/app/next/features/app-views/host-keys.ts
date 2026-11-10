// The host's keys for the views of apps (HRN-808, keys.on/press/hints): a view runs the shortcut commands of a screen
// with the keys the person set for them, a key pressed inside it runs as the host's would, and it may ask how the host
// writes a command's key. The same work for a frame and for a builtin app (Sinopia) in the page's own process.
import { APP_KEY_SCOPES, type IBridgeHandlers } from '@/next/features/app-views/bridge';
import { keyHintOf } from '@/next/features/keys/keys-help';
import { listenKeys, runChord } from '@/next/features/keys/use-keys';

/**
 * The key handlers of a view. `focused` says whether a press the view reports may be the person's: a frame's only
 * while the frame has the focus, so a view in the background cannot press the host's keys. `own` is the host's own
 * app (Sinopia's boards pass on every key, the global ones too); a frame's press runs only the actions of the screens
 * an app may run (APP_KEY_SCOPES), never the host's global ones (a new chat, copying an answer).
 */
export const hostKeys = (
  focused: () => boolean,
  own = false,
): Required<Pick<IBridgeHandlers, 'keys' | 'keyPress' | 'keyHints'>> => ({
  keys: (scope, handlers) => listenKeys(scope, handlers),
  keyPress: (chord, typing) => focused() && runChord(chord, typing, own ? undefined : APP_KEY_SCOPES),
  keyHints: (commands) => Object.fromEntries(commands.map((c) => [c, keyHintOf(c)])),
});
