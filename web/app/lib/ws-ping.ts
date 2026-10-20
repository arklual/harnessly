/**
 * The page's own heartbeat on a socket: a browser cannot see the protocol pings the server sends, so a
 * socket the network dropped without a word looks open to it for minutes (a phone switching networks, a proxy that
 * forgot the connection). The page sends this text and expects the answer back the whole way through the gateway.
 */
export const PING_FRAME = '{"type":"ping"}';
export const PONG_FRAME = '{"type":"pong"}';

export const isPingFrame = (raw: unknown): boolean => String(raw) === PING_FRAME;
