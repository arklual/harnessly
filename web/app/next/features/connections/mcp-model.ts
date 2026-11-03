// What the MCP part of Settings → Connections says about a server, a catalog entry and a refusal, in the person's words.
import {
  DEFAULT_HEADER,
  MCP_AUTH,
  MCP_REASONS,
  MCP_STATE,
  type IMcpCatalogEntry,
  type IMcpProbe,
  type IMcpServer,
} from '@/next/features/connections/mcp-api';
import { hostOf } from '@/next/features/connections/model';
import { locale, plural, t } from '@/next/i18n';

const known = (list: readonly string[], id: string): boolean => list.some((item) => item === id);

/** What a refusal of the runtime (or a sign-in window's reason) says to the person. */
export const mcpReasonText = (code: string): string =>
  known(MCP_REASONS, code)
    ? t(`next.connections.mcp.reason.${code}`)
    : t('next.connections.mcp.reason.other', { code });

/** The entry's one line in the person's language; the English one when there is no other. */
export const blurbOf = (entry: IMcpCatalogEntry): string =>
  locale() === 'ru' && entry.blurbRu !== '' ? entry.blurbRu : entry.blurb;

/** The server needs the person (a sign-in or a token) before agents can use it. */
export const needsPerson = (server: IMcpServer): boolean =>
  server.state === MCP_STATE.signin || server.state === MCP_STATE.denied;

/** The server takes a typed token: it signs in by a header, or by OAuth that lets no new app register. */
export const takesToken = (server: IMcpServer): boolean =>
  server.auth === MCP_AUTH.header || server.oauthUnavailable;

/** A sign-in the person has not finished yet: said calmly, it is not a failure. */
export const isQuietState = (server: IMcpServer): boolean =>
  server.state === MCP_STATE.signin && !takesToken(server) && server.tools.length === 0;

/** What the server's last answer found, when it is not fine; null when it is (nothing to say then). */
export const mcpStateText = (server: IMcpServer): string | null => {
  if (server.state === MCP_STATE.ok) return null;
  if (server.state === MCP_STATE.unreachable) return t('next.connections.mcp.state.unreachable');
  if (server.oauthUnavailable) return t('next.connections.mcp.state.oauthUnavailable');
  if (server.auth === MCP_AUTH.header) return t('next.connections.mcp.state.token');
  return t('next.connections.mcp.state.signin');
};

/** «mcp.linear.app · as anna@acme.com»; «off for agents» when it is turned off. */
export const mcpSubLine = (server: IMcpServer): string => {
  const where =
    server.me === ''
      ? hostOf(server.url)
      : t('next.connections.mcp.as', { host: hostOf(server.url), who: server.me });
  return server.enabled ? where : t('next.connections.mcp.offLine', { where });
};

export const toolsText = (count: number): string => plural(count, 'next.connections.mcp.tools');

/** How a probed server signs in, in one line. */
export const probeLine = (probe: IMcpProbe): string => {
  if (probe.auth === MCP_AUTH.oauth) return t('next.connections.mcp.signs.oauth');
  if (probe.auth === MCP_AUTH.header) return t('next.connections.mcp.signs.header');
  return t('next.connections.mcp.signs.none');
};

/**
 * A pasted bare token goes as «Bearer <token>» in Authorization; anything with a scheme already, or for another
 * header, goes as typed.
 */
export const headerValue = (name: string, typed: string): string => {
  const value = typed.trim();
  if (name.trim().toLowerCase() !== DEFAULT_HEADER.toLowerCase() || /\s/.test(value)) return value;
  return `Bearer ${value}`;
};

/** The catalog without what is connected already. */
export const openCatalog = (
  catalog: readonly IMcpCatalogEntry[],
  servers: readonly IMcpServer[],
): IMcpCatalogEntry[] => catalog.filter((entry) => !servers.some((server) => server.catalogId === entry.id));

/** The list with `server` in it: in its old place when it was there, else last. */
export const withServer = (all: readonly IMcpServer[], server: IMcpServer): IMcpServer[] =>
  all.some((item) => item.id === server.id)
    ? all.map((item) => (item.id === server.id ? server : item))
    : [...all, server];
