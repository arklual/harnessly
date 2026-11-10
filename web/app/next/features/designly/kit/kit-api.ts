// The kit screen over the runtime (HRN-897): the kits with a canvas, a theme's canvas (internal/designkit/canvas.go,
// the same shapes), the kit's files and prepared atlases, a template as a project, where a kit is used and which kit
// a project is built on.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

export interface IKitTheme {
  id: string;
  title: string;
}

export interface IKitCard {
  id: string;
  title: string;
  version: string;
  versions: string[];
  themes: IKitTheme[];
  /** A picture of the kit (a path in the version), '' when none. */
  cover: string;
  coverTheme: string;
  layouts: number;
  assets: number;
  /** A card per theme (a kit of several brands shows each on the shelf), in the kit's order. */
  themeCards?: IKitThemeCard[];
}

export interface IKitThemeCard {
  id: string;
  title: string;
  /** The theme's cover (a path in the version), '' when none. */
  cover: string;
  layouts: number;
}

export interface IColorToken {
  id: string;
  name: string;
  /** A value per mode, or one under ''. */
  values: Record<string, string>;
  usage?: string;
}

export interface ITypeStyle {
  id: string;
  group: string;
  name: string;
  family: string;
  size: string;
  line?: string;
  weight?: string;
}

export interface IPlainToken {
  id: string;
  kind: string;
  name: string;
  value: string;
  usage?: string;
}

export interface IKitProp {
  name: string;
  type: string;
  values?: string[];
  required?: boolean;
  description?: string;
}

export type TPropValue = string | number | boolean | null | Record<string, unknown> | unknown[];

export interface IKitVariant {
  id: string;
  name: string;
  props?: Record<string, TPropValue>;
}

export interface IKitComponent {
  id: string;
  node: string;
  name: string;
  description?: string;
  props: IKitProp[];
  variants: IKitVariant[];
  sample?: Record<string, TPropValue>;
  preview?: string;
  /** Its picture in the theme, drawn when it is not live; `cell` is its place in the group's atlas, -1 without one. */
  thumb?: string;
  cell: number;
  /** What it is a part of: a parent component's id, or 'provider' for one that draws nothing itself. */
  part?: string;
  keywords?: string[];
}

export interface IKitComponentGroup {
  id: string;
  title: string;
  components: IKitComponent[];
  /** The components' pictures, one sheet per group. */
  atlas?: IAtlas;
}

export interface IAtlas {
  file: string;
  cellW: number;
  cellH: number;
  cols: number;
  count: number;
}

export interface IKitAsset {
  node: string;
  name: string;
  file: string;
  dark?: string;
  /** The colour it is meant to sit on (a white logo on a dark one), absent for any. */
  bg?: string;
  /** Its cell in the group's atlas, -1 when the atlas does not hold it. */
  cell: number;
}

export interface IKitAssetGroup {
  id: string;
  node: string;
  title: string;
  kind: string;
  component?: string;
  prop?: string;
  items: IKitAsset[];
  atlas?: IAtlas;
}

export interface IKitSlot {
  key: string;
  kind: string;
  /** What the field is called to a person («Заголовок», «Картинка 2»), absent in an older kit. */
  label?: string;
  sample?: string;
}

export interface IKitLayout {
  id: string;
  node: string;
  name: string;
  purpose?: string;
  props?: Record<string, TPropValue>;
  scripts?: string[];
  board?: string;
  thumb?: string;
  slots: IKitSlot[];
  cell: number;
  /** How a deck takes it (`<kit>/<theme>/<layout>`), '' when the theme does not ship its board. */
  address?: string;
}

export interface IKitTemplateSection {
  id: string;
  node: string;
  title: string;
  layouts: IKitLayout[];
}

export interface IKitTemplate {
  id: string;
  node: string;
  /** The tree section it lives in: templates, or reference for a template's guide (absent in an older kit). */
  section?: string;
  title: string;
  kind: string;
  aspect: [number, number];
  component?: string;
  cover?: string;
  description?: string;
  sections: IKitTemplateSection[];
  atlas?: IAtlas;
}

export interface IIndexEntry {
  node: string;
  kind: string;
  title: string;
  path: string;
  words: string;
}

export interface IKitCanvas {
  kit: string;
  version: string;
  versions: string[];
  title: string;
  theme: string;
  themes: IKitTheme[];
  namespace: string;
  /** The theme's folder in the version ('' for a kit that is one theme). */
  base: string;
  sections: { id: string; title: string }[];
  modes: string[] | null;
  colors: IColorToken[] | null;
  type: ITypeStyle[] | null;
  fonts: { family: string; file: string; weight?: string }[] | null;
  tokens: IPlainToken[] | null;
  groups: IKitComponentGroup[] | null;
  assets: IKitAssetGroup[] | null;
  templates: IKitTemplate[] | null;
  index: IIndexEntry[] | null;
  preparedAt: number;
}

export interface IKitBoardUse {
  path: string;
  title: string;
  nodes: string[];
}

export interface IKitLink {
  project: string;
  title: string;
  kit: string;
  kitTitle: string;
  theme: string;
  /** '' when the project's copy matches no installed version. */
  version: string;
  latest: string;
  boards: IKitBoardUse[];
  /** Every node the boards use: its name, and its picture as a path in the version where it has one. */
  nodes: Record<string, { name: string; shot?: string }>;
}

export const fetchKitCards = async (m: MachineId, signal?: AbortSignal): Promise<IKitCard[]> =>
  (await runtimeJson<{ kits: IKitCard[] }>(m, platformPath('/design/kit/canvases'), { signal })).kits;

/** The first open of a theme makes its atlases inside the request, which a busy machine may take a minute over. */
const KIT_CANVAS_TIMEOUT_MS = 120_000;

export const fetchKitCanvas = (
  m: MachineId,
  kit: string,
  version: string | null,
  theme: string | null,
  signal?: AbortSignal,
) => {
  const q = new URLSearchParams({ kit });
  if (version) q.set('version', version);
  if (theme) q.set('theme', theme);
  return runtimeJson<IKitCanvas>(
    m,
    platformPath(`/design/kit/canvas?${q.toString()}`),
    { signal },
    KIT_CANVAS_TIMEOUT_MS,
  );
};

/** A file of the version, by its path in the version (the theme's base joined in by the caller). */
export const kitFileUrl = (m: MachineId, kit: string, version: string, path: string) =>
  runtimePath(
    platformPath(
      `/design/kit/file?kit=${encodeURIComponent(kit)}&version=${encodeURIComponent(version)}&path=${encodeURIComponent(path)}`,
    ),
    m,
  );

/** A kit card's picture, '' when the kit has none. */
/** The sections a template can live in, in the tree's order. */
export const TEMPLATE_SECTIONS = ['templates', 'reference'] as const;

/** The templates of a tree section: a template without one is in «Шаблоны». */
export const templatesIn = (c: Pick<IKitCanvas, 'templates'>, section: string): IKitTemplate[] =>
  (c.templates ?? []).filter((t) => (t.section || 'templates') === section);

export const kitCover = (m: MachineId, card: Pick<IKitCard, 'id' | 'version' | 'cover'>) =>
  card.cover ? kitFileUrl(m, card.id, card.version, card.cover) : '';

/** One card of the shelf: a theme of a kit, or the whole kit when it is one theme. */
export interface IKitShelfCard {
  key: string;
  kit: string;
  version: string;
  /** The theme to open, null for the kit's first. */
  theme: string | null;
  title: string;
  /** The kit's title under a theme's (a kit of several themes), '' otherwise. */
  kitTitle: string;
  cover: string;
  layouts: number;
}

/** The shelf's cards: a kit of several themes shows each theme with its own cover, so what it holds is seen at once. */
export const shelfCards = (cards: readonly IKitCard[]): IKitShelfCard[] =>
  cards.flatMap((c): IKitShelfCard[] => {
    const themes = c.themeCards ?? [];
    if (themes.length < 2)
      return [
        {
          key: c.id,
          kit: c.id,
          version: c.version,
          theme: null,
          title: c.title,
          kitTitle: '',
          cover: c.cover,
          layouts: c.layouts,
        },
      ];
    return themes.map((th) => ({
      key: `${c.id}/${th.id}`,
      kit: c.id,
      version: c.version,
      theme: th.id,
      title: th.title,
      kitTitle: c.title,
      cover: th.cover,
      layouts: th.layouts,
    }));
  });

/** A file of the canvas's theme (a path relative to the theme's folder). */
export const themeFileUrl = (m: MachineId, c: Pick<IKitCanvas, 'kit' | 'version' | 'base'>, rel: string) =>
  kitFileUrl(m, c.kit, c.version, c.base ? `${c.base}/${rel}` : rel);

/** A prepared atlas of the canvas's theme. */
export const prepUrl = (
  m: MachineId,
  c: Pick<IKitCanvas, 'kit' | 'version' | 'theme' | 'preparedAt'>,
  file: string,
) =>
  runtimePath(
    platformPath(
      `/design/kit/file?kit=${encodeURIComponent(c.kit)}&version=${encodeURIComponent(c.version)}&theme=${encodeURIComponent(c.theme)}&prep=${encodeURIComponent(file)}&at=${c.preparedAt}`,
    ),
    m,
  );

/** «Открыть как проект»: the template as a project of boards (the one made before, while it is there). */
export const openKitProject = (
  m: MachineId,
  body: { kit: string; version: string; theme: string; template: string },
) => runtimeJson<{ id: string; title: string }>(m, platformPath('/design/kit/project'), postJson(body));

export const fetchKitUsage = async (m: MachineId, kit: string, signal?: AbortSignal): Promise<IKitLink[]> =>
  (
    await runtimeJson<{ projects: IKitLink[] }>(
      m,
      platformPath(`/design/kit/usage?kit=${encodeURIComponent(kit)}`),
      { signal },
    )
  ).projects;

export const fetchKitLink = async (
  m: MachineId,
  artifact: string,
  signal?: AbortSignal,
): Promise<IKitLink | null> =>
  (
    await runtimeJson<{ link: IKitLink | null }>(
      m,
      platformPath(`/design/kit/link?artifact=${encodeURIComponent(artifact)}`),
      { signal },
    )
  ).link;

/** Where a kit's board lives on the library server, for a frame in the preview slot. */
export const kitViewPath = (c: Pick<IKitCanvas, 'kit' | 'version' | 'theme'>, file: string) =>
  `/kitv/${encodeURIComponent(c.kit)}/${encodeURIComponent(c.version)}/${encodeURIComponent(c.theme)}/${file}`;

/** A component drawn live with these props (the panel's parameters), on the library server. */
export const kitPlayPath = (
  c: Pick<IKitCanvas, 'kit' | 'version' | 'theme'>,
  component: string,
  props: Record<string, TPropValue>,
  scripts: readonly string[] = [],
) => {
  const q = new URLSearchParams({ c: component, p: JSON.stringify(props) });
  for (const s of scripts) q.append('s', s);
  return `${kitViewPath(c, '_play.dc.html')}?${q.toString()}`;
};
