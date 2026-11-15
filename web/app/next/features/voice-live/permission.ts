// The agent of a spoken task asks for a permission (during a call the voice-guard hook asks before an action
// that goes outside): the voice says what is asked and waits for «да» or «нет» instead of a card the person cannot see.
import type { Permission } from '@/next/protocol/v2';

/** A command longer than this is not read out: nobody listens to a whole script. */
const SAY_MAX = 80;

/** The open permission the voice may answer: a tool's, with both a «once» and a «deny» to choose. */
export const voicePermission = (list: readonly Permission[]): Permission | null =>
  list.find(
    (p) =>
      p.kind === 'tool' &&
      p.options.some((o) => o.scope === 'once') &&
      p.options.some((o) => o.scope === 'deny'),
  ) ?? null;

/** Several commands in one, a redirect or a substitution: a «да» heard over it would allow more than was said. */
const CHAINED_RE = /[\n\r;&|`<>]|\$\(/;

/** What the agent wants to do, whole: its command, or the tool's name. */
export const permissionWhat = (p: Permission): string =>
  (p.command ?? p.tool ?? '').replace(/\s+/g, ' ').trim();

/** The voice may ask a «да» only for what it says whole: a short single command. A long or chained one is decided on
 *  the screen, where the whole of it is seen. */
export const sayable = (p: Permission): boolean => {
  const what = permissionWhat(p);
  return what.length <= SAY_MAX && !CHAINED_RE.test(p.command ?? '');
};

/** The option a «да» or a «нет» picks. */
export const permissionOption = (p: Permission, allow: boolean): string | null =>
  p.options.find((o) => o.scope === (allow ? 'once' : 'deny'))?.id ?? null;
