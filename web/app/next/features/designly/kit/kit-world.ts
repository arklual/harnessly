// The kit's canvas laid out: sections one under another, each a frame with its groups, every group a grid of
// cells laid out by itself (nothing is moved by hand). The main view shows the kit with each template as a stack; a
// template's own view is its grid of layouts by its sections. Pure: the screen draws what this answers, and finds a
// node's place, the cells a viewport sees, and the tree's rows here.
import {
  TEMPLATE_SECTIONS,
  templatesIn,
  type IKitCanvas,
  type IKitComponent,
  type IKitLayout,
  type IKitTemplate,
  type TPropValue,
} from '@/next/features/designly/kit/kit-api';

/** A component's part that only provides context and draws nothing (designkit.PartProvider). */
export const PART_PROVIDER = 'provider';

/** Whether the live preview can draw a component: it has a sample or a variant and is not a provider (`preview` is
 * an optional hint of older kits). The same props `harnessly design kit render` draws its picture with. */
export function drawsLive(c: IKitComponent): boolean {
  return (
    c.part !== PART_PROVIDER && (c.preview !== undefined || c.sample !== undefined || c.variants.length > 0)
  );
}

/** The props a component is drawn with by default: its sample with the first variant over it. */
export function liveProps(c: IKitComponent): Record<string, TPropValue> {
  return { ...(c.sample ?? {}), ...(c.variants[0]?.props ?? {}) };
}

export const ITEM = {
  color: 'color',
  type: 'type',
  token: 'token',
  component: 'component',
  stack: 'stack',
  layout: 'layout',
  icon: 'icon',
  image: 'image',
  logo: 'logo',
} as const;
export type TItem = (typeof ITEM)[keyof typeof ITEM];

export interface IRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface IWorldItem {
  node: string;
  kind: TItem;
  label: string;
  /** The second line of a card (a colour's value, a style's size, a layout's purpose). */
  sub?: string;
  /** Where the picture comes from: a file of the theme and a cell of an atlas (a key of `atlases`). */
  file?: string;
  atlas?: string;
  cell?: number;
  /** A colour's fill. */
  fill?: string;
  /** The colour a picture is meant to sit on (a white logo on a dark one). */
  bg?: string;
  /** A component that takes an asset by name (Icon) shows a few of them: cells of that group's atlas. */
  samples?: { atlas: string; cells: number[] };
  /** A text style's sample: its family, size in pixels and weight. */
  font?: { family: string; px: number; weight: string };
}

/** A grid of cells: row by row, a caption under each cell. */
export interface IBlock {
  id: string;
  x: number;
  y: number;
  cols: number;
  cellW: number;
  cellH: number;
  gap: number;
  captionH: number;
  items: IWorldItem[];
}

export interface IFrame {
  node: string;
  title: string;
  /** A section's frame (0) or a group's title inside it (1). */
  depth: 0 | 1;
  /** A big branch says how many it holds. */
  count?: number;
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface IWorld {
  frames: IFrame[];
  blocks: IBlock[];
  w: number;
  h: number;
}

export const VIEW = { main: 'main', template: 'template' } as const;
export type TView = { kind: typeof VIEW.main } | { kind: typeof VIEW.template; id: string };
export const MAIN_VIEW: TView = { kind: VIEW.main };

const PAD = 72;
const SECTION_GAP = 200;
/** How many of its assets a component's card shows (a grid of 6 × 3). */
const SAMPLE_CELLS = 18;
const GROUP_GAP = 96;
const GROUP_TITLE = 56;
const SECTION_TITLE = 0;

interface IGridSpec {
  cols: number;
  cellW: number;
  cellH: number;
  gap: number;
  captionH: number;
}

const GRID: Record<TItem, IGridSpec> = {
  [ITEM.color]: { cols: 8, cellW: 200, cellH: 140, gap: 24, captionH: 56 },
  [ITEM.type]: { cols: 1, cellW: 1240, cellH: 96, gap: 16, captionH: 32 },
  [ITEM.token]: { cols: 6, cellW: 240, cellH: 96, gap: 24, captionH: 48 },
  [ITEM.component]: { cols: 3, cellW: 560, cellH: 340, gap: 40, captionH: 56 },
  [ITEM.stack]: { cols: 3, cellW: 640, cellH: 360, gap: 64, captionH: 64 },
  [ITEM.layout]: { cols: 8, cellW: 384, cellH: 216, gap: 28, captionH: 44 },
  [ITEM.icon]: { cols: 24, cellW: 96, cellH: 96, gap: 16, captionH: 28 },
  [ITEM.image]: { cols: 8, cellW: 240, cellH: 240, gap: 24, captionH: 36 },
  [ITEM.logo]: { cols: 6, cellW: 320, cellH: 160, gap: 24, captionH: 36 },
};

const ASSET_ITEM: Readonly<Record<string, TItem>> = { icon: ITEM.icon, logo: ITEM.logo, image: ITEM.image };

const gridOf = (kind: TItem, template?: IKitTemplate): IGridSpec => {
  const g = GRID[kind];
  if (!template || (kind !== ITEM.layout && kind !== ITEM.stack)) return g;
  const [aw, ah] = template.aspect;
  return { ...g, cellH: Math.round((g.cellW * ah) / aw) };
};

const blockSize = (b: Pick<IBlock, 'cols' | 'cellW' | 'cellH' | 'gap' | 'captionH'>, n: number) => {
  const cols = Math.max(1, Math.min(b.cols, n));
  const rows = Math.ceil(n / cols);
  return {
    w: cols * b.cellW + (cols - 1) * b.gap,
    h: rows * (b.cellH + b.captionH) + Math.max(0, rows - 1) * b.gap,
  };
};

/** The place of the i-th cell of a block (the picture, without its caption). */
export const cellRect = (b: IBlock, i: number): IRect => {
  const col = i % b.cols;
  const row = Math.floor(i / b.cols);
  return {
    x: b.x + col * (b.cellW + b.gap),
    y: b.y + row * (b.cellH + b.captionH + b.gap),
    w: b.cellW,
    h: b.cellH,
  };
};

/** The cells of a block a rectangle of the world sees: the range of indices, empty when none. */
export const visibleCells = (
  b: IBlock,
  view: IRect,
): { from: number; to: number; c0: number; c1: number } | null => {
  const stepX = b.cellW + b.gap;
  const stepY = b.cellH + b.captionH + b.gap;
  const rows = Math.ceil(b.items.length / b.cols);
  const r0 = Math.max(0, Math.floor((view.y - b.y) / stepY));
  const r1 = Math.min(rows - 1, Math.floor((view.y + view.h - b.y) / stepY));
  const c0 = Math.max(0, Math.floor((view.x - b.x) / stepX));
  const c1 = Math.min(b.cols - 1, Math.floor((view.x + view.w - b.x) / stepX));
  if (r1 < r0 || c1 < c0) return null;
  return { from: r0 * b.cols, to: Math.min(b.items.length - 1, r1 * b.cols + b.cols - 1), c0, c1 };
};

const pxOf = (size: string): number => {
  const n = Number.parseFloat(size);
  if (!Number.isFinite(n)) return 24;
  return size.endsWith('rem') || size.endsWith('em') ? n * 16 : n;
};

const colorValue = (values: Record<string, string>, mode: string | null): string =>
  (mode !== null ? values[mode] : undefined) ?? values[''] ?? values.light ?? Object.values(values)[0] ?? '';

/** A layout as a cell of a grid. */
const layoutItem = (l: IKitLayout, template: IKitTemplate): IWorldItem => ({
  node: l.node,
  kind: ITEM.layout,
  label: l.name,
  ...(l.purpose ? { sub: l.purpose } : {}),
  ...(l.thumb ? { file: l.thumb } : {}),
  ...(l.cell >= 0 ? { atlas: `t:${template.id}`, cell: l.cell } : {}),
});

const coverOf = (t: IKitTemplate): IKitLayout | null => {
  for (const s of t.sections) for (const l of s.layouts) if (l.id === t.cover) return l;
  return t.sections[0]?.layouts[0] ?? null;
};

export const layoutCount = (t: IKitTemplate): number => t.sections.reduce((n, s) => n + s.layouts.length, 0);

interface IGroupSpec {
  node: string;
  title: string;
  count?: number;
  kind: TItem;
  items: IWorldItem[];
  template?: IKitTemplate;
}

interface ISectionSpec {
  node: string;
  title: string;
  count?: number;
  groups: IGroupSpec[];
}

/** The default names of the canvas's sections, by id; a kit may name them itself. */
export type TSectionTitles = Record<string, string>;

const mainSections = (c: IKitCanvas, titles: TSectionTitles, mode: string | null): ISectionSpec[] => {
  const out: ISectionSpec[] = [];
  const named = (id: string) => c.sections.find((s) => s.id === id)?.title || titles[id] || id;
  const has = (id: string) => c.sections.some((s) => s.id === id);
  if (has('foundation')) {
    const groups: IGroupSpec[] = [];
    const colors = c.colors ?? [];
    if (colors.length > 0)
      groups.push({
        node: 'group:colors',
        title: titles.colors ?? 'colors',
        kind: ITEM.color,
        items: colors.map((col) => ({
          node: col.id,
          kind: ITEM.color,
          label: col.name,
          sub: colorValue(col.values, mode),
          fill: colorValue(col.values, mode),
        })),
      });
    const type = c.type ?? [];
    if (type.length > 0)
      groups.push({
        node: 'group:type',
        title: titles.type ?? 'type',
        kind: ITEM.type,
        items: type.map((s) => ({
          node: s.id,
          kind: ITEM.type,
          label: `${s.group} · ${s.name}`,
          sub: [s.size, s.line, s.weight].filter(Boolean).join(' / '),
          font: { family: s.family, px: pxOf(s.size), weight: s.weight ?? '400' },
        })),
      });
    const tokens = c.tokens ?? [];
    if (tokens.length > 0)
      groups.push({
        node: 'group:tokens',
        title: titles.tokens ?? 'tokens',
        kind: ITEM.token,
        items: tokens.map((tk) => ({ node: tk.id, kind: ITEM.token, label: tk.name, sub: tk.value })),
      });
    if (groups.length > 0) out.push({ node: 'section:foundation', title: named('foundation'), groups });
  }
  if (has('components')) {
    // A part of another component (TableRow of Table) is no tile of its own: its parent's panel lists it. A provider
    // draws nothing, so the providers go together into one group at the end instead of empty tiles among the rest.
    // A component that draws the items of an asset group (Icon over the icons) shows a few of them as its samples.
    const samplesOf = (k: IKitComponent): Pick<IWorldItem, 'samples'> => {
      const source = (c.assets ?? []).find((a) => a.component === k.name || a.component === k.id);
      const cells = (source?.items ?? [])
        .map((a) => a.cell)
        .filter((n) => n >= 0)
        .slice(0, SAMPLE_CELLS);
      return source && cells.length > 0 ? { samples: { atlas: `a:${source.id}`, cells } } : {};
    };
    const tile = (k: IKitComponent): IWorldItem => ({
      node: k.node,
      kind: ITEM.component,
      label: k.name,
      ...(k.variants.length > 0 ? { sub: k.variants.map((v) => v.name).join(' · ') } : {}),
      ...(k.thumb ? { file: k.thumb } : {}),
      ...samplesOf(k),
    });
    const groups = (c.groups ?? [])
      .map<IGroupSpec>((g) => ({
        node: `cgroup:${g.id}`,
        title: g.title,
        kind: ITEM.component,
        items: g.components
          .filter((k) => !k.part)
          .map((k) => ({
            ...tile(k),
            ...(g.atlas && k.cell >= 0 ? { atlas: `c:${g.id}`, cell: k.cell } : {}),
          })),
      }))
      .filter((g) => g.items.length > 0);
    const providers = (c.groups ?? []).flatMap((g) => g.components.filter((k) => k.part === PART_PROVIDER));
    if (providers.length > 0)
      groups.push({
        node: 'cgroup:_providers',
        title: titles.providers ?? 'providers',
        kind: ITEM.component,
        items: providers.map(tile),
      });
    if (groups.length > 0) out.push({ node: 'section:components', title: named('components'), groups });
  }
  for (const sid of TEMPLATE_SECTIONS) {
    const templates = templatesIn(c, sid);
    if (!has(sid) || templates.length === 0) continue;
    out.push({
      node: `section:${sid}`,
      title: named(sid),
      groups: [
        {
          node: `group:${sid}`,
          title: '',
          kind: ITEM.stack,
          template: templates[0],
          items: templates.map((tp) => {
            const cover = coverOf(tp);
            return {
              node: tp.node,
              kind: ITEM.stack,
              label: tp.title,
              sub: String(layoutCount(tp)),
              ...(cover?.thumb ? { file: cover.thumb } : {}),
              ...(cover && cover.cell >= 0 ? { atlas: `t:${tp.id}`, cell: cover.cell } : {}),
            };
          }),
        },
      ],
    });
  }
  if (has('assets')) {
    const groups = (c.assets ?? []).map<IGroupSpec>((g) => {
      const kind = ASSET_ITEM[g.kind] ?? ITEM.image;
      return {
        node: g.node,
        title: g.title,
        count: g.items.length,
        kind,
        items: g.items.map((a) => ({
          node: a.node,
          kind,
          label: a.name,
          file: a.file,
          ...(a.bg ? { bg: a.bg } : {}),
          ...(a.cell >= 0 ? { atlas: `a:${g.id}`, cell: a.cell } : {}),
        })),
      };
    });
    if (groups.length > 0) out.push({ node: 'section:assets', title: named('assets'), groups });
  }
  return out;
};

const templateSections = (t: IKitTemplate): ISectionSpec[] => [
  {
    node: t.node,
    title: t.title,
    count: layoutCount(t),
    groups: t.sections.map((s) => ({
      node: s.node,
      title: s.title,
      count: s.layouts.length,
      kind: ITEM.layout,
      template: t,
      items: s.layouts.map((l) => layoutItem(l, t)),
    })),
  },
];

const place = (sections: ISectionSpec[]): IWorld => {
  const frames: IFrame[] = [];
  const blocks: IBlock[] = [];
  let y = 0;
  let maxW = 0;
  for (const s of sections) {
    const top = y;
    let gy = top + PAD + SECTION_TITLE;
    let w = 0;
    for (const g of s.groups) {
      const spec = gridOf(g.kind, g.template);
      const titled = g.title !== '';
      const by = gy + (titled ? GROUP_TITLE : 0);
      const size = blockSize(spec, g.items.length);
      const block: IBlock = {
        id: g.node,
        x: PAD,
        y: by,
        ...spec,
        cols: Math.min(spec.cols, Math.max(1, g.items.length)),
        items: g.items,
      };
      blocks.push(block);
      if (titled)
        frames.push({
          node: g.node,
          title: g.title,
          depth: 1,
          ...(g.count ? { count: g.count } : {}),
          x: PAD,
          y: gy,
          w: size.w,
          h: by - gy + size.h,
        });
      w = Math.max(w, size.w);
      gy = by + size.h + GROUP_GAP;
    }
    const h = gy - GROUP_GAP + PAD - top;
    frames.push({
      node: s.node,
      title: s.title,
      depth: 0,
      ...(s.count ? { count: s.count } : {}),
      x: 0,
      y: top,
      w: w + PAD * 2,
      h,
    });
    maxW = Math.max(maxW, w + PAD * 2);
    y = top + h + SECTION_GAP;
  }
  // Sections are as wide as the widest, so the column reads as one.
  for (const f of frames) if (f.depth === 0) f.w = maxW;
  return { frames, blocks, w: maxW, h: Math.max(0, y - SECTION_GAP) };
};

/** The world of a view: the kit with its templates as stacks, or one template's grid by its sections. */
export const buildWorld = (
  c: IKitCanvas,
  view: TView,
  titles: TSectionTitles,
  mode: string | null,
): IWorld => {
  if (view.kind === VIEW.template) {
    const t = (c.templates ?? []).find((x) => x.id === view.id);
    if (t) return place(templateSections(t));
  }
  return place(mainSections(c, titles, mode));
};

/** The view a node lives in: a template's layouts and sections in the template's, the rest in the main one. */
export const viewOfNode = (node: string): TView => {
  const m = /^(?:layout|tsection):([^/]+)\//.exec(node);
  return m?.[1] ? { kind: VIEW.template, id: m[1] } : MAIN_VIEW;
};

export const sameView = (a: TView, b: TView): boolean =>
  a.kind === b.kind && (a.kind === VIEW.main || (b.kind === VIEW.template && a.id === b.id));

/** A node's place in a world: a cell's picture, a frame, or null when the world does not hold it. */
export const rectOf = (w: IWorld, node: string): IRect | null => {
  for (const f of w.frames) if (f.node === node) return { x: f.x, y: f.y, w: f.w, h: f.h };
  for (const b of w.blocks) {
    const i = b.items.findIndex((it) => it.node === node);
    if (i >= 0) {
      const r = cellRect(b, i);
      return { ...r, h: r.h + b.captionH };
    }
  }
  return null;
};

/** The item under a point of the world (its picture or its caption). */
export const itemAt = (w: IWorld, x: number, y: number): IWorldItem | null => {
  for (const b of w.blocks) {
    const stepX = b.cellW + b.gap;
    const stepY = b.cellH + b.captionH + b.gap;
    const col = Math.floor((x - b.x) / stepX);
    const row = Math.floor((y - b.y) / stepY);
    if (col < 0 || col >= b.cols || row < 0) continue;
    if (x - b.x - col * stepX > b.cellW || y - b.y - row * stepY > b.cellH + b.captionH) continue;
    const it = b.items[row * b.cols + col];
    if (it) return it;
  }
  return null;
};

/** The frame (a section or a group) under a point, the innermost first. */
export const frameAt = (w: IWorld, x: number, y: number): IFrame | null => {
  let found: IFrame | null = null;
  for (const f of w.frames)
    if (x >= f.x && x <= f.x + f.w && y >= f.y && y <= f.y + f.h && (!found || f.depth > found.depth))
      found = f;
  return found;
};

/** The layouts of a template in its order, for the single view's arrows. */
export const templateLayouts = (t: IKitTemplate): IKitLayout[] => t.sections.flatMap((s) => s.layouts);
