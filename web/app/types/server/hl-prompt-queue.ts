// Shapes of what the runtime answers; the runtime itself is Go.

/**
 * Messages sent while the agent works wait here, on the machine, the way Codex keeps its queue: shown above
 * the composer, sent by the server once the turn ends, steered in at once on request. Closing the browser loses
 * nothing, and the first message of an agent that is still starting or waking up waits here too.
 */
export interface IQueuedPrompt {
  id: string;
  text: string;
  /** Uploaded pictures, pasted before the text. */
  images: string[];
  createdAt: number;
  /** The page's own key of the message: the runtime answers a repeated add with the message it already queued. */
  clientId?: string;
  /** Delivery gave up on the message: it stays here, and only the person sends it again or drops it. */
  status?: 'failed';
  reason?: TQueueFailReason;
  /** When the person last asked for the failed message again. */
  retriedAt?: number;
}

/** Why the runtime gave up: the launch failed, nothing to resume, the tab is gone, it waited a day, typing it kept
 * failing, or the agent input held someone else’s text. */
export type TQueueFailReason =
  'launch-failed' | 'no-session' | 'no-tab' | 'expired' | 'not-delivered' | 'input-occupied' | (string & {});
