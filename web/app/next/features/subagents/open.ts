// «Открыть» on a subagent: the feed, the right column of background work and anything else that names a subagent ask
// for it here, and the right pane of the open chat — the one place that can show it — hears. One bus, because the
// asker is far from the pane in the tree and the open chat has one pane.
type TListener = (taskId: string) => void;

const listeners = new Set<TListener>();

/** Opens the subagent in the open chat's right pane: `taskId` is the agent's id (a background task's id). */
export const openSubagent = (taskId: string): void => {
  listeners.forEach((listen) => listen(taskId));
};

/** The pane listens while it is mounted; the returned function stops it. */
export const onOpenSubagent = (listen: TListener): (() => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};
