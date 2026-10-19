export const verifyTokenValue = (value) => value === globalThis.__golden.cliToken;
export const getCliToken = () => globalThis.__golden.cliToken;
