const g = () => globalThis.__golden;
export const checkTerminalProcess = async (session) => g().fronts[session] ?? { isSafe: true, processName: 'zsh' };
export const sendLiteralThenEnter = async (session, text) => { g().typed.push({ session, text, via: 'type' }); };
