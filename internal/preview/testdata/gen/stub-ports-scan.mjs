export const findPortListener = async (port) => globalThis.__golden?.listeners?.[port] ?? null;
export const listPreviewPorts = async () => [];
export const resolvePortRoot = async () => null;
