// An MCP Apps card in the feed: a tool of an app's own MCP server returns a result that names a view,
// `_meta.ui.resourceUri: "ui://<app id>/<view>"`, and the data to draw it with. Read off the tool step's output.
import { APP_TARGET_RE } from '@/next/features/app-views/target';

export interface IAppCard {
  app: string;
  view: string;
  data: unknown;
  /** The plugin and server the tool came from, as the agent names them (mcp__plugin_<plugin>_<server>__<tool>). */
  plugin: string;
  server: string;
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

const URI_RE = /^ui:\/\/([a-z0-9][a-z0-9-]{0,39}\.[a-z0-9][a-z0-9-]{0,39})\/([a-z0-9][a-z0-9_-]{0,39})$/;

/** An app's plugin name: its id with '.' as '-' (internal/apps PluginName). */
export const pluginName = (id: string): string => id.replaceAll('.', '-');

/**
 * `mcp__plugin_<plugin>_<server>__<tool>` as the runtime reads it (internal/apps/policy.go mcpParts): the plugin has no
 * '_', so it ends at the first one; the server is the rest up to the '__' that opens the tool.
 */
export const pluginToolOf = (tool: string): { plugin: string; server: string } | null => {
  const rest = tool.startsWith('mcp__') ? tool.slice(5) : null;
  const cut = rest === null ? -1 : rest.indexOf('__');
  if (rest === null || cut < 1) return null;
  const owner = rest.slice(0, cut);
  if (!owner.startsWith('plugin_')) return null;
  const [plugin, ...server] = owner.slice(7).split('_');
  return plugin !== '' && server.length > 0 && server.join('_') !== ''
    ? { plugin, server: server.join('_') }
    : null;
};

/**
 * The card a tool step names; null for any other step. Whether the tool is the app's own is decided where the app is
 * known (`isOwnTool`): the plugin must be the app's exactly and the server one of the package's .mcp.json servers.
 */
export const parseAppCard = (tool: string, output: string | null): IAppCard | null => {
  if (output === null || !output.trimStart().startsWith('{')) return null;
  const from = pluginToolOf(tool);
  if (from === null) return null;
  let body: unknown;
  try {
    body = JSON.parse(output);
  } catch {
    return null;
  }
  if (!isRecord(body) || !isRecord(body._meta) || !isRecord(body._meta.ui)) return null;
  const uri = body._meta.ui.resourceUri;
  const m = typeof uri === 'string' ? URI_RE.exec(uri) : null;
  if (!m || !APP_TARGET_RE.test(m[1])) return null;
  return { app: m[1], view: m[2], data: body.structuredContent ?? body.data ?? null, ...from };
};

/** The card's tool is the app's own: its plugin is the app's, and the server is one of the app's. */
export const isOwnTool = (card: IAppCard, app: { id: string; mcpServers: readonly string[] }): boolean =>
  card.app === app.id && card.plugin === pluginName(app.id) && app.mcpServers.includes(card.server);
