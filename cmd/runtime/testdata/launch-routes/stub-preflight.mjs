const result = () => ({ installed: true, version: '1.0.0', binaryPath: '/usr/bin/agent' });
export const runClaudePreflight = async () => result();
export const runCodexPreflight = async () => result();
