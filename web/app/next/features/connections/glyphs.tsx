// A product's mark on the Connections page: its logo in white on the product's own colour (styles/connections.css,
// .hx-conn-glyph[data-kind] / [data-mcp]). Something without a logo in brand-marks.ts — a server of the person's own, a
// product added later — gets its name's first letter on a neutral tile.
import { BRAND_MARK, type IBrandMark, type TBrand } from '@/next/features/connections/brand-marks';

const isBrand = (id: string): id is TBrand => Object.hasOwn(BRAND_MARK, id);

const BrandMark = ({ mark }: { mark: IBrandMark }) => (
  <svg
    className="hx-conn-glyph__logo"
    viewBox={mark.box ?? '0 0 24 24'}
    fill="currentColor"
    fillRule={mark.evenOdd === true ? 'evenodd' : undefined}
    data-faded={mark.faded === true ? '' : undefined}
    focusable="false"
  >
    {mark.d.map((d) => (
      <path key={d} d={d} />
    ))}
  </svg>
);

/** The logo when there is one, else the first letter of the name. */
const Mark = ({ brand, name }: { brand: TBrand | null; name: string }) =>
  brand === null ? name.trim().slice(0, 1).toUpperCase() : <BrandMark mark={BRAND_MARK[brand]} />;

interface IGlyphProps {
  kind: string;
  size?: 'sm' | 'md';
}

const ProductGlyph = ({ kind, size = 'md' }: IGlyphProps) => (
  <span className="hx-conn-glyph" data-kind={kind} data-size={size} aria-hidden="true">
    <Mark brand={isBrand(kind) ? kind : null} name={kind} />
  </span>
);

export default ProductGlyph;

/** Catalog servers (mcpconn's catalog ids) whose logo has another key in brand-marks.ts. */
const MCP_BRAND: Readonly<Record<string, TBrand>> = { 'cloudflare-docs': 'cloudflare' };

const mcpBrand = (id: string): TBrand | null => {
  if (Object.hasOwn(MCP_BRAND, id)) return MCP_BRAND[id];
  return isBrand(id) ? id : null;
};

interface IMcpGlyphProps {
  /** The catalog id; empty for a server of the person's own. */
  id: string;
  name: string;
  size?: 'sm' | 'md';
}

/** An MCP server's mark: the catalog server's logo on its colour, else the name's first letter on a neutral tile. */
export const McpGlyph = ({ id, name, size = 'md' }: IMcpGlyphProps) => (
  <span className="hx-conn-glyph" data-mcp={id === '' ? undefined : id} data-size={size} aria-hidden="true">
    <Mark brand={mcpBrand(id)} name={name} />
  </span>
);
