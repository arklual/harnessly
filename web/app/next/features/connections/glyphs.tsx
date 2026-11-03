// A product's mark on the Connections page: a letter mark in the product's own colour (styles/connections.css,
// .hx-conn-glyph[data-kind]). No vendor logos are drawn: the colour and the letters are enough to tell them apart.
import { isKnownKind, type TKind } from '@/next/features/connections/model';

const MARK = {
  jira: 'J',
  confluence: 'C',
  youtrack: 'YT',
  linear: 'L',
  gitlab: 'GL',
  github: 'GH',
  gitea: 'Gt',
  bitbucket: 'Bb',
  redmine: 'R',
  'yandex-tracker': 'Y',
  'azure-devops': 'Az',
  notion: 'N',
  mediawiki: 'W',
  outline: 'O',
  bookstack: 'B',
  wikijs: 'Wj',
} as const satisfies Record<TKind, string>;

interface IGlyphProps {
  kind: string;
  size?: 'sm' | 'md';
}

const ProductGlyph = ({ kind, size = 'md' }: IGlyphProps) => (
  <span className="hx-conn-glyph" data-kind={kind} data-size={size} aria-hidden="true">
    {isKnownKind(kind) ? MARK[kind] : kind.slice(0, 1).toUpperCase()}
  </span>
);

export default ProductGlyph;

/** The catalog's MCP servers (mcpconn's catalog ids); a server of the person's own gets its name's first letter. */
const MCP_MARK = {
  atlassian: 'A',
  linear: 'L',
  notion: 'N',
  sentry: 'S',
  github: 'GH',
  asana: 'As',
  stripe: 'S',
  vercel: 'V',
  supabase: 'Sb',
  'cloudflare-docs': 'Cf',
  context7: 'C7',
  huggingface: 'HF',
  figma: 'F',
  intercom: 'I',
  monday: 'M',
  webflow: 'W',
  paypal: 'PP',
  square: 'Sq',
} as const;
type TMcpMarked = keyof typeof MCP_MARK;

const isMarked = (id: string): id is TMcpMarked => Object.hasOwn(MCP_MARK, id);

interface IMcpGlyphProps {
  /** The catalog id; empty for a server of the person's own. */
  id: string;
  name: string;
  size?: 'sm' | 'md';
}

/** An MCP server's mark: the catalog's letters on the product's colour (.hx-conn-glyph[data-mcp]), else a neutral one. */
export const McpGlyph = ({ id, name, size = 'md' }: IMcpGlyphProps) => (
  <span className="hx-conn-glyph" data-mcp={id === '' ? undefined : id} data-size={size} aria-hidden="true">
    {isMarked(id) ? MCP_MARK[id] : name.trim().slice(0, 1).toUpperCase()}
  </span>
);
