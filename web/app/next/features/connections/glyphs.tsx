// A product's mark on the Connections page: its logo in white on the product's own colour (styles/connections.css,
// .hx-conn-glyph[data-kind] / [data-mcp]). Something without a logo in brand-marks.ts — a server of the person's own, a
// product added later — gets its name's first letter on a neutral tile.
import type { CSSProperties } from 'react';
import { BRAND_MARK, type IBrandMark, type TBrand } from '@/next/features/connections/brand-marks';
import { MARK_OF, MORE_MARK } from '@/next/features/connections/brand-marks-more';

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
  size?: 'sm' | 'md' | 'lg';
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
  /** The character of a service of one's own's mark; the name's first letter when empty. */
  letter?: string;
  size?: 'sm' | 'md' | 'lg';
}

/** The brand's colour is light (white would not read on it) or dark (it vanishes on a dark page). */
const tone = (hex: string): 'light' | 'dark' | undefined => {
  const [r, g, b] = [0, 2, 4].map((at) => parseInt(hex.slice(at, at + 2), 16) / 255);
  const luma = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  if (luma > 0.62) return 'light';
  return luma < 0.12 ? 'dark' : undefined;
};

/** A colour of the name's own for a service with no logo: the same name always gets the same one. */
const nameHue = (name: string): number => {
  let sum = 0;
  for (const char of name.trim().toLocaleLowerCase()) sum = (sum * 31 + (char.codePointAt(0) ?? 0)) % 360;
  return sum;
};

const YANDEX_RED = '#fc3f1d';

/** The mark of a service with a logo in the more-marks file, on the brand's colour. */
const moreMark = (id: string) => {
  if (!Object.hasOwn(MARK_OF, id)) return null;
  const key = MARK_OF[id];
  return Object.hasOwn(MORE_MARK, key) ? MORE_MARK[key] : null;
};

/**
 * An MCP server's mark: the logo of a catalog service on its colour (the hand-picked ones in connections.css, the others
 * from brand-marks-more.ts), the Yandex services' «Я» on Yandex red, and for the rest the name's first letter on a colour
 * the name gives, so a list of them is not a column of grey tiles.
 */
export const McpGlyph = ({ id, name, letter = '', size = 'md' }: IMcpGlyphProps) => {
  const brand = mcpBrand(id);
  if (brand !== null)
    return (
      <span className="hx-conn-glyph" data-mcp={id} data-size={size} aria-hidden="true">
        <Mark brand={brand} name={name} />
      </span>
    );
  const more = moreMark(id);
  if (more !== null)
    return (
      <span
        className="hx-conn-glyph"
        data-size={size}
        data-tone={tone(more.hex)}
        style={{ '--hx-conn-c': `#${more.hex}` } as CSSProperties}
        aria-hidden="true"
      >
        <BrandMark mark={{ d: more.d }} />
      </span>
    );
  if (id.startsWith('yandex'))
    return (
      <span
        className="hx-conn-glyph"
        data-size={size}
        style={{ '--hx-conn-c': YANDEX_RED } as CSSProperties}
        aria-hidden="true"
      >
        Я
      </span>
    );
  return (
    <span
      className="hx-conn-glyph"
      data-size={size}
      data-letter=""
      style={{ '--hx-conn-c': `hsl(${nameHue(id === '' ? name : id)} 48% 42%)` } as CSSProperties}
      aria-hidden="true"
    >
      {(letter === '' ? name.trim().slice(0, 1) : letter).toLocaleUpperCase()}
    </span>
  );
};
