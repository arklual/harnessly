export interface ISendKeyInput {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}

/** ⌘↵ / Ctrl+↵ in the composer of a working agent: the message goes to the agent now, plain Enter queues it. */
export const isSendNowKey = (event: ISendKeyInput): boolean =>
  event.key === 'Enter' && (event.metaKey || event.ctrlKey) && !event.shiftKey && !event.altKey;

export const sendNowShortcut = (mac: boolean): string => (mac ? '⌘↵' : 'Ctrl+↵');

export type TComposerRoute = 'queue' | 'send-now' | 'direct';

/**
 * Where the composer's message goes. A message to a working, starting or waking agent waits in the server's queue, and
 * so does one to an agent the page has not seen running (`unverified`: the process check has not answered, or the state
 * it holds is a left-over of a runtime restart): typed straight into the pane it would land in the shell. The server
 * checks the agent before typing and brings a gone one back with `--resume`. ⌘↵ sends on at once, but only to an agent
 * that works: one that is starting has nothing to read it yet. A slash line and a Codex tab (no server queue) go to the
 * agent directly.
 */
export const composerRoute = ({
  queueOnServer,
  slashLine,
  holding,
  resumable,
  agentWorking,
  unverified = false,
  now,
}: {
  queueOnServer: boolean;
  slashLine: boolean;
  holding: boolean;
  resumable: boolean;
  agentWorking: boolean;
  unverified?: boolean;
  now: boolean;
}): TComposerRoute => {
  if (!queueOnServer || slashLine || !(holding || resumable || agentWorking || unverified)) return 'direct';
  return now && agentWorking && !holding && !resumable && !unverified ? 'send-now' : 'queue';
};
