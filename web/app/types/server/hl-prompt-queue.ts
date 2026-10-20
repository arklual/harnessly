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
}
