export const broadcastSessionStats = (stats) => globalThis.__golden.broadcasts.push(JSON.stringify(stats));
