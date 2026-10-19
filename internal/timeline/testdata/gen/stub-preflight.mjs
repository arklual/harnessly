export const getShellPath = async () => process.env.PATH ?? '';
export const runCodexPreflight = async () => ({ installed: true, version: '0.1.0', binaryPath: '/usr/local/bin/codex' });
