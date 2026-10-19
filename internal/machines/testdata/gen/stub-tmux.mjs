export const getAllPanesInfo = async () => new Map((globalThis.__golden?.panes ?? []).map((pid, i) => [`s${i}`, { pid }]));
