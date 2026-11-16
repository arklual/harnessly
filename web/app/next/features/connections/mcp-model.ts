// What the MCP part of Settings → Connections says about a server, a catalog entry and a refusal, in the person's words.
import {
  DEFAULT_HEADER,
  MCP_AUTH,
  MCP_KINDS,
  MCP_REASONS,
  MCP_STATE,
  type IMcpCatalogEntry,
  type IMcpProbe,
  type IMcpServer,
  type TMcpKind,
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
 * A pasted bare token goes as «Bearer <token>» in Authorization, or after the catalog entry's own prefix («OAuth »
 * for Yandex 360) when it has one; anything with a scheme already, or for another header, goes as typed.
 */
export const headerValue = (name: string, typed: string, prefix: string): string => {
  const value = typed.trim();
  if (name.trim().toLowerCase() !== DEFAULT_HEADER.toLowerCase() || /\s/.test(value)) return value;
  return `${prefix === '' ? 'Bearer ' : prefix}${value}`;
};

/** The catalog without what is connected already. */
export const openCatalog = (
  catalog: readonly IMcpCatalogEntry[],
  servers: readonly IMcpServer[],
): IMcpCatalogEntry[] => catalog.filter((entry) => !servers.some((server) => server.catalogId === entry.id));

/** The entry's name, words and address hold every word of the query, case aside. */
const matches = (entry: IMcpCatalogEntry, words: readonly string[]): boolean => {
  const text = [
    entry.name,
    entry.blurb,
    entry.blurbRu,
    hostOf(entry.url),
    t(`next.connections.mcp.kind.${entry.kind}`),
  ]
    .join(' ')
    .toLocaleLowerCase();
  return words.every((word) => text.includes(word));
};

export interface ICatalogGroup<T> {
  kind: TMcpKind;
  items: T[];
}

/** The catalog in its groups, in MCP_KINDS order, with only what the query finds; empty groups are left out. */
export const catalogGroups = <T>(
  items: readonly T[],
  entryOf: (item: T) => IMcpCatalogEntry,
  query: string,
): ICatalogGroup<T>[] => {
  const words = query
    .toLocaleLowerCase()
    .split(/\s+/)
    .filter((word) => word !== '');
  const found = items.filter((item) => matches(entryOf(item), words));
  return MCP_KINDS.map((kind) => ({
    kind,
    items: found.filter((item) => entryOf(item).kind === kind),
  })).filter((group) => group.items.length > 0);
};

/** The list with `server` in it: in its old place when it was there, else last. */
export const withServer = (all: readonly IMcpServer[], server: IMcpServer): IMcpServer[] =>
  all.some((item) => item.id === server.id)
    ? all.map((item) => (item.id === server.id ? server : item))
    : [...all, server];
