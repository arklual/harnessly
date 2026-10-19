const g = globalThis;
export const readAllPanes = async () => new Map(Object.entries(g.__golden?.panes ?? {}));
export const capturePaneContentWithHistory = async (session, lines) => g.__golden?.capture?.[session] ?? null;
export const getDescendantPids = async () => [];
