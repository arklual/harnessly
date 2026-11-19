// What the page of one service says (Settings → Connections → a service): which catalog entry and which kept server it
// is about, the text of the entry in the person's language, the steps to connect, the links, and — for a connected
// server — what its real tools let an agent do.
import {
  MCP_AUTH,
  MCP_LINK,
  MCP_STATE,
  type IMcpCatalogEntry,
  type IMcpLink,
  type IMcpServer,
  type IMcpTool,
  type TMcpLink,
} from '@/next/features/connections/mcp-api';
import {
  blurbOf,
  isKeyed,
  mcpStateText,
  needsPerson,
  takesToken,
} from '@/next/features/connections/mcp-model';
import { locale, t } from '@/next/i18n';

/** What an address of a service names: the catalog's entry, the person's server, or both (a connected catalog service). */
export interface IService {
  entry: IMcpCatalogEntry | null;
  server: IMcpServer | null;
}

/** The address of a service's page: the catalog id when it has one, else the id of the person's own server. */
export const itemOf = (server: IMcpServer): string =>
  server.catalogId === '' ? server.id : server.catalogId;

/**
 * The page's entry of a service of the person's own: made of the record the machine keeps (its description, its keys,
 * where it signs in), so the page of one reads like a catalog service's.
 */
export const ownEntry = (server: IMcpServer): IMcpCatalogEntry | null => {
  const own = server.custom;
  if (own === null) return null;
  return {
    id: server.catalogId,
    name: server.name,
    kind: own.category,
    url: server.url,
    transport: server.transport,
    auth: server.auth,
    headerName: server.headerName,
    tokenPrefix: '',
    tokenUrl: '',
    docsUrl: '',
    blurb: own.about,
    blurbRu: own.aboutRu,
    env: server.env,
    runtime: '',
    guide: server.guide,
    about: own.about,
    aboutRu: own.aboutRu,
    can: own.can,
    canRu: own.canRu,
    steps: own.steps,
    stepsRu: own.stepsRu,
    links: own.links,
  };
};

/** The service an address names; null when it names neither a server nor a catalog entry. */
export const findService = (
  item: string,
  servers: readonly IMcpServer[],
  catalog: readonly IMcpCatalogEntry[],
): IService | null => {
  const byId = servers.find((server) => server.id === item) ?? null;
  const entryId = byId === null ? item : byId.catalogId;
  const server = byId ?? servers.find((one) => one.catalogId === item) ?? null;
  const entry =
    entryId === ''
      ? null
      : (catalog.find((one) => one.id === entryId) ?? (server === null ? null : ownEntry(server)));
  if (entry === null && server === null) return null;
  return { entry, server };
};

const ru = (): boolean => locale() === 'ru';

/**
 * What the service is, for the head of its page: the first sentence of the long description is the one line under the
 * name, the rest is the paragraph under it. An entry with no long description has its one-line Blurb and no paragraph.
 */
export const aboutParts = (entry: IMcpCatalogEntry): { line: string; rest: string } => {
  const long = ru() && entry.aboutRu !== '' ? entry.aboutRu : entry.about;
  if (long === '') return { line: blurbOf(entry), rest: '' };
  const cut = /^(.+?[.!?])\s+(\S[\s\S]*)$/.exec(long);
  return cut === null ? { line: long, rest: '' } : { line: cut[1], rest: cut[2] };
};

/** The entry's «what an agent can do», in the person's language; empty when the catalog has none. */
export const canOf = (entry: IMcpCatalogEntry): readonly string[] => {
  if (ru() && entry.canRu.length > 0) return entry.canRu;
  return entry.can;
};

/** The way to connect, as numbered steps: the catalog's own, or the ones every sign-in kind has. */
export const stepsOf = (entry: IMcpCatalogEntry): readonly string[] => {
  const own = ru() && entry.stepsRu.length > 0 ? entry.stepsRu : entry.steps;
  if (own.length > 0) return own;
  if (isKeyed(entry)) {
    const getKeys =
      entry.tokenUrl === ''
        ? t('next.connections.svc.steps.keysNoLink')
        : t('next.connections.svc.steps.keys');
    return [getKeys, t('next.connections.svc.steps.keysPaste')];
  }
  if (entry.auth === MCP_AUTH.oauth)
    return [
      t('next.connections.svc.steps.oauth1', { name: entry.name }),
      t('next.connections.svc.steps.oauth2'),
      t('next.connections.svc.steps.oauth3'),
    ];
  if (entry.auth === MCP_AUTH.header)
    return [
      entry.tokenUrl === ''
        ? t('next.connections.svc.steps.tokenNoLink')
        : t('next.connections.svc.steps.token1'),
      t('next.connections.svc.steps.token2'),
    ];
  return [t('next.connections.svc.steps.none')];
};

const LINK_ORDER: readonly TMcpLink[] = [
  MCP_LINK.docs,
  MCP_LINK.setup,
  MCP_LINK.token,
  MCP_LINK.home,
  MCP_LINK.pricing,
];

/** The entry's official links: its guide's, then the docs and token pages the entry has always had; no address twice. */
export const linksOf = (entry: IMcpCatalogEntry): readonly IMcpLink[] => {
  const all: IMcpLink[] = [...entry.links];
  if (entry.docsUrl !== '') all.push({ kind: MCP_LINK.docs, url: entry.docsUrl });
  if (entry.tokenUrl !== '') all.push({ kind: MCP_LINK.token, url: entry.tokenUrl });
  const seen = new Set<string>();
  const unique = all.filter((link) => !seen.has(link.url) && seen.add(link.url));
  return LINK_ORDER.flatMap((kind) => unique.filter((link) => link.kind === kind));
};

/** A link's name on the page: what it is, not its address. */
export const linkName = (kind: TMcpLink): string => t(`next.connections.svc.link.${kind}`);

/** The host a link goes to, for the small line under its name. */
export const linkHost = (url: string): string => {
  try {
    return new URL(url).host.replace(/^www\./, '');
  } catch {
    return url;
  }
};

export interface IToolGroup {
  /** The thing the tools are about («issue», «page»), as the tools' names say it. */
  noun: string;
  /** What can be done with it («list», «create»), in the order the tools come. */
  verbs: readonly string[];
  count: number;
}

const WORD = /[_\-.:/ ]+/;

/** The tool's name as a verb and what it works on: «create_issue» is «create» an «issue». */
const split = (name: string): { verb: string; noun: string } | null => {
  const words = name
    .replace(/([a-z])([A-Z])/g, '$1_$2')
    .toLowerCase()
    .split(WORD)
    .filter((word) => word !== '');
  if (words.length < 2) return null;
  const [verb, ...rest] = words;
  const noun = rest.join(' ').replace(/(?<=[a-z]{3})(es|s)$/, '');
  return { verb, noun };
};

/** The tools by what they work on, the biggest groups first. Names that do not split that way are left out. */
export const toolGroups = (tools: readonly IMcpTool[]): IToolGroup[] => {
  const groups = new Map<string, string[]>();
  for (const tool of tools) {
    const parts = split(tool.name);
    if (parts === null) continue;
    const verbs = groups.get(parts.noun) ?? [];
    if (!verbs.includes(parts.verb)) verbs.push(parts.verb);
    groups.set(parts.noun, verbs);
  }
  return [...groups.entries()]
    .map(([noun, verbs]) => ({ noun, verbs, count: verbs.length }))
    .sort((a, b) => b.count - a.count);
};

/** The first sentence of a tool's description, cut to a line. */
const firstSentence = (text: string): string => {
  const one = text.replace(/\s+/g, ' ').trim();
  const end = /[.!?](\s|$)/.exec(one);
  const sentence = end === null ? one : one.slice(0, end.index + 1);
  return sentence.length > 110 ? `${sentence.slice(0, 107).trimEnd()}…` : sentence;
};

/**
 * Three to five lines of what the connected server lets an agent do, from its real tools: the things they work on with
 * what can be done to each, and when the names say too little, the first sentences of the tools' own descriptions.
 */
export const canFromTools = (tools: readonly IMcpTool[]): string[] => {
  const groups = toolGroups(tools).filter((group) => group.count > 1 || tools.length <= 8);
  if (groups.length >= 3 && tools.length > 5)
    return groups
      .slice(0, 5)
      .map(
        (group) => `${group.noun}: ${group.verbs.slice(0, 4).join(', ')}${group.verbs.length > 4 ? '…' : ''}`,
      );
  const described = tools.filter((tool) => tool.description.trim() !== '');
  const step = Math.max(1, Math.floor(described.length / 5));
  const lines: string[] = [];
  for (let at = 0; at < described.length && lines.length < 5; at += step) {
    const line = firstSentence(described[at].description);
    if (!lines.includes(line)) lines.push(line);
  }
  return lines;
};

export const TONE = { ok: 'ok', warn: 'warn', off: 'off', wait: 'wait' } as const;
export type TTone = (typeof TONE)[keyof typeof TONE];

export interface IStatus {
  tone: TTone;
  /** «Online», «Needs sign-in», …: one or two words for the pill. */
  label: string;
  /** Why, in a sentence, when it is not fine. */
  detail: string | null;
}

/** The server's state for the pill on its row and the card on its page. */
export const statusOf = (server: IMcpServer): IStatus => {
  const detail = mcpStateText(server);
  if (!server.enabled) return { tone: TONE.off, label: t('next.connections.svc.status.off'), detail: null };
  if (server.state === MCP_STATE.unreachable)
    return { tone: TONE.warn, label: t('next.connections.svc.status.unreachable'), detail };
  if (needsPerson(server) || detail !== null) {
    const label =
      takesToken(server) || isKeyed(server)
        ? t('next.connections.svc.status.keys')
        : t('next.connections.svc.status.signin');
    const quiet = server.state === MCP_STATE.signin && server.tools.length === 0;
    return { tone: quiet ? TONE.wait : TONE.warn, label, detail };
  }
  return { tone: TONE.ok, label: t('next.connections.svc.status.ok'), detail: null };
};
