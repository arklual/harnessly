/**
 * Where an agent's launch stands: from `claude` (or `claude --resume`) typed into the pane until its prompt
 * box takes input. A message sent meanwhile must wait in the server's queue: typed into the pane it lands in the
 * shell or in a Claude that is still drawing its screen, and its Enter is lost.
 */
export type TLaunchPhase = 'starting' | 'ready' | 'failed';

/** A launch whose process never showed up, or that showed up and went away, is given this long to be noticed. */
export const LAUNCH_NO_PROCESS_MS = 15_000;
/** A resumed 50 MB transcript takes a while; past this the CLI is treated as not coming up. */
export const LAUNCH_TIMEOUT_MS = 150_000;

export interface ILaunchProbe {
  elapsedMs: number;
  /** The agent's process is under the pane right now. */
  running: boolean;
  /** It has been seen running since the launch. */
  seenRunning: boolean;
  /** Its prompt box is drawn and takes input. */
  promptReady: boolean;
}

export const launchPhase = ({ elapsedMs, running, seenRunning, promptReady }: ILaunchProbe): TLaunchPhase => {
  if (running && promptReady) return 'ready';
  if (seenRunning && !running) return 'failed';
  if (!running && elapsedMs >= LAUNCH_NO_PROCESS_MS) return 'failed';
  return elapsedMs >= LAUNCH_TIMEOUT_MS ? 'failed' : 'starting';
};

/** What the page tells about a tab's launch; null when none is on. */
export interface ITabLaunch {
  since: number;
  failed: boolean;
}

export const tabLaunchOf = (entry: {
  launchingSince?: number | null;
  launchFailed?: boolean;
}): ITabLaunch | null =>
  typeof entry.launchingSince === 'number'
    ? { since: entry.launchingSince, failed: entry.launchFailed === true }
    : null;

/** The composer holds what is typed (queues it) while an agent starts: the page's own flags or the server's launch. */
export const composerHolds = (launch: ITabLaunch | null, localHolding: boolean): boolean =>
  localHolding || (launch !== null && !launch.failed);

/** What the queue above the composer says about the launch: calm while it runs, an error once it gave up. */
export const queueLaunchOf = (launch: ITabLaunch | null, holding: boolean): 'starting' | 'failed' | null =>
  launch?.failed ? 'failed' : holding ? 'starting' : null;

export type TQueueAgentAction = 'launching' | 'deliver' | 'resume' | 'unrecoverable';

/**
 * What a tab with a queued message needs from its agent. Typing into a pane whose agent is gone lands in the shell
 * (a zsh prompt even looks like an input line), so a gone agent is resumed first and the message waits for it.
 * `launchGaveUp`: the launch under way has failed; it is not tried again until someone asks (the composer, a kill).
 */
export const queueAgentAction = ({
  launching,
  launchGaveUp,
  agentRunning,
  hasSession,
}: {
  launching: boolean;
  launchGaveUp: boolean;
  agentRunning: boolean;
  hasSession: boolean;
}): TQueueAgentAction => {
  if (launching || launchGaveUp) return 'launching';
  if (agentRunning) return 'deliver';
  return hasSession ? 'resume' : 'unrecoverable';
};
