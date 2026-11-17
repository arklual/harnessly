// The person's own look as data (docs/looks.md): the same fields and limits as internal/looks/look.go. A
// look never holds CSS or code: every field is a number in a range or one of a closed list, and readLook drops a
// field that is neither, so a look from anywhere (an agent, another device) can only recolour the page.
//
// The prefs key hx-looks keeps them all: the one for every chat, one per folder of the sidebar and one per chat. A chat
// wears its own, else its folder's, else the one for all; none is the app as it always was.
import presetsJson from '@/next/features/looks/presets.json';

export const LOOK_MODES = ['auto', 'light', 'dark'] as const;
export const BG_KINDS = ['none', 'color', 'gradient', 'image', 'scene'] as const;
export const SCENES = [
  'matrix',
  'stars',
  'aurora',
  'rain',
  'synthwave',
  'twinkle',
  'paper',
  'lava',
  'snow',
  'topo',
] as const;
export const RADII = ['sharp', 'default', 'round'] as const;
export const DENSITIES = ['compact', 'cozy'] as const;
export const FONTS = ['system', 'inter', 'serif', 'mono', 'rounded'] as const;
export const BUBBLES = ['soft', 'solid', 'outline', 'glass'] as const;
export const TURNS = ['plain', 'card', 'glass'] as const;
export const COMPOSERS = ['solid', 'glass', 'outline'] as const;
export const SIDEBARS = ['plain', 'tinted', 'glass'] as const;
export const FITS = ['cover', 'tile'] as const;
/** The built-in live effects (features/looks/fx/*.js). */
export const FX_PRESETS = ['snowplay', 'nightlamp', 'laser', 'newyear', 'elves', 'weather'] as const;
export type TFxPreset = (typeof FX_PRESETS)[number];

export type TLookMode = (typeof LOOK_MODES)[number];
export type TBgKind = (typeof BG_KINDS)[number];
export type TScene = (typeof SCENES)[number];

/** One dot of the picker: a hue in degrees and how strong it is, 0–100. */
export interface IHue {
  h: number;
  s: number;
}

export interface ILookBg {
  kind: TBgKind;
  /** The sha256 of a picture kept by /api/v2/me/looks (kind image). */
  image?: string;
  scene?: TScene;
  fit?: (typeof FITS)[number];
  /** How fast a scene moves, 0–100 (50 when absent). */
  speed?: number;
}

export interface ILookParts {
  bubble?: (typeof BUBBLES)[number];
  turns?: (typeof TURNS)[number];
  composer?: (typeof COMPOSERS)[number];
  sidebar?: (typeof SIDEBARS)[number];
}

/** A live effect over the chat (docs/looks.md, «Живые эффекты»): a built-in one or the hash of the person's own script,
 *  kept like a picture. Its code runs only in a sandboxed frame that sees the pointer, the blocks' boxes and the agent's
 *  state, never the page or the chat's text (fx-layer.tsx). */
export interface ILookFx {
  preset?: TFxPreset;
  /** The sha256 of a script kept by /api/v2/me/looks. */
  script?: string;
  /** Also run under the chat, as a background (hx.layer «back»). */
  back?: boolean;
  /** How much of it, 0–100 (60 when absent). */
  power?: number;
}

export interface ILook {
  v: 1;
  name?: string;
  mode: TLookMode;
  hues: readonly IHue[];
  intensity: number;
  accent?: IHue;
  bg: ILookBg;
  grain: number;
  dim: number;
  blur: number;
  radius?: (typeof RADII)[number];
  density?: (typeof DENSITIES)[number];
  font?: (typeof FONTS)[number];
  parts: ILookParts;
  fx?: ILookFx;
}

/** Everything the prefs keep under hx-looks. */
export interface ILooks {
  v: 1;
  all: ILook | null;
  folders: Readonly<Record<string, ILook>>;
  chats: Readonly<Record<string, ILook>>;
}

export const NO_LOOKS: ILooks = { v: 1, all: null, folders: {}, chats: {} };

/** How many folders and chats keep a look of their own: the prefs' value stays under the hub's 32 KB. */
export const MAX_SCOPED = 40;
export const MAX_NAME = 60;

export interface IPreset {
  id: string;
  name: string;
  nameEn: string;
  look: ILook;
}

const isObj = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);
const num = (v: unknown, lo: number, hi: number): number | null =>
  typeof v === 'number' && Number.isFinite(v) && v >= lo && v <= hi ? v : null;
const oneOf = <T extends string>(list: readonly T[], v: unknown): T | undefined =>
  list.find((item) => item === v);

const readHue = (v: unknown): IHue | null => {
  if (!isObj(v)) return null;
  const h = num(v.h, 0, 360);
  const s = num(v.s, 0, 100);
  return h === null || s === null ? null : { h, s };
};

const HASH_RE = /^[0-9a-f]{64}$/;

/** The live effect of a look; undefined for none or for one that does not read. */
export const readFx = (raw: unknown): ILookFx | undefined => {
  if (!isObj(raw)) return undefined;
  const preset = oneOf(FX_PRESETS, raw.preset);
  const script = typeof raw.script === 'string' && HASH_RE.test(raw.script) ? raw.script : undefined;
  if ((preset === undefined) === (script === undefined)) return undefined;
  const fx: ILookFx = preset ? { preset } : { script };
  if (raw.back === true) fx.back = true;
  const power = num(raw.power, 0, 100);
  if (power !== null) fx.power = power;
  return fx;
};

/** A look out of anything (the prefs, an agent's request); null when it is none. Unknown fields are dropped. */
export const readLook = (raw: unknown): ILook | null => {
  if (!isObj(raw) || raw.v !== 1) return null;
  const mode = oneOf(LOOK_MODES, raw.mode);
  const hues = Array.isArray(raw.hues) ? raw.hues.map(readHue) : [];
  if (!mode || hues.length < 1 || hues.length > 3 || hues.some((h) => h === null)) return null;
  const intensity = num(raw.intensity, 0, 100);
  const grain = num(raw.grain, 0, 100);
  const dim = num(raw.dim, 0, 100);
  const blur = num(raw.blur, 0, 24);
  if (intensity === null || grain === null || dim === null || blur === null) return null;
  const bgRaw = isObj(raw.bg) ? raw.bg : {};
  const kind = oneOf(BG_KINDS, bgRaw.kind) ?? 'none';
  const bg: ILookBg = { kind };
  if (kind === 'image') {
    if (typeof bgRaw.image !== 'string' || !HASH_RE.test(bgRaw.image)) return null;
    bg.image = bgRaw.image;
    bg.fit = oneOf(FITS, bgRaw.fit) ?? 'cover';
  }
  if (kind === 'scene') {
    const scene = oneOf(SCENES, bgRaw.scene);
    if (!scene) return null;
    bg.scene = scene;
  }
  const speed = num(bgRaw.speed, 0, 100);
  if (speed !== null) bg.speed = speed;
  const partsRaw = isObj(raw.parts) ? raw.parts : {};
  const accent = raw.accent === undefined ? undefined : readHue(raw.accent);
  if (accent === null) return null;
  const look: ILook = {
    v: 1,
    mode,
    hues: hues as IHue[],
    intensity,
    bg,
    grain,
    dim,
    blur,
    parts: {
      bubble: oneOf(BUBBLES, partsRaw.bubble),
      turns: oneOf(TURNS, partsRaw.turns),
      composer: oneOf(COMPOSERS, partsRaw.composer),
      sidebar: oneOf(SIDEBARS, partsRaw.sidebar),
    },
    radius: oneOf(RADII, raw.radius),
    density: oneOf(DENSITIES, raw.density),
    font: oneOf(FONTS, raw.font),
  };
  if (accent) look.accent = accent;
  const fx = readFx(raw.fx);
  if (fx) look.fx = fx;
  if (typeof raw.name === 'string' && raw.name.trim())
    look.name = [...raw.name.trim()].slice(0, MAX_NAME).join('');
  return look;
};

const readMap = (raw: unknown): Record<string, ILook> => {
  const out: Record<string, ILook> = {};
  if (!isObj(raw)) return out;
  for (const [id, value] of Object.entries(raw).slice(0, MAX_SCOPED)) {
    const look = readLook(value);
    if (look && id.length <= 200) out[id] = look;
  }
  return out;
};

/** The prefs' value of hx-looks; the empty set for none or for a value that does not read. */
export const readLooks = (raw: string | null): ILooks => {
  if (!raw) return NO_LOOKS;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isObj(parsed)) return NO_LOOKS;
    return {
      v: 1,
      all: readLook(parsed.all),
      folders: readMap(parsed.folders),
      chats: readMap(parsed.chats),
    };
  } catch {
    return NO_LOOKS;
  }
};

export const isEmpty = (looks: ILooks): boolean =>
  looks.all === null && Object.keys(looks.folders).length === 0 && Object.keys(looks.chats).length === 0;

/** The look a place wears: the chat's, else its folder's, else the one for all; null is the app as it always was. */
export const lookFor = (looks: ILooks, chatId: string | null, folderId: string | null): ILook | null =>
  (chatId !== null ? looks.chats[chatId] : undefined) ??
  (folderId !== null ? looks.folders[folderId] : undefined) ??
  looks.all;

export type TLookScope = { k: 'all' } | { k: 'folder'; id: string } | { k: 'chat'; id: string };

/** The set with one scope's look put on (a look) or taken away (null). */
export const withLook = (looks: ILooks, scope: TLookScope, look: ILook | null): ILooks => {
  if (scope.k === 'all') return { ...looks, all: look };
  const key = scope.k === 'folder' ? 'folders' : 'chats';
  const next: Record<string, ILook> = { ...looks[key] };
  delete next[scope.id];
  if (look) next[scope.id] = look;
  // The oldest go first past the limit (an object keeps its insertion order).
  const ids = Object.keys(next);
  for (const id of ids.slice(0, Math.max(0, ids.length - MAX_SCOPED))) delete next[id];
  return { ...looks, [key]: next };
};

export const scopeLook = (looks: ILooks, scope: TLookScope): ILook | null => {
  if (scope.k === 'all') return looks.all;
  return (scope.k === 'folder' ? looks.folders : looks.chats)[scope.id] ?? null;
};

export const PRESETS: readonly IPreset[] = (presetsJson as unknown[]).flatMap((raw) => {
  if (!isObj(raw)) return [];
  const look = readLook(raw.look);
  return look && typeof raw.id === 'string' && typeof raw.name === 'string'
    ? [{ id: raw.id, name: raw.name, nameEn: typeof raw.nameEn === 'string' ? raw.nameEn : raw.name, look }]
    : [];
});

/** A fresh look to start the picker from: the app's own calm, one violet dot. */
export const STARTER_LOOK: ILook = {
  v: 1,
  mode: 'auto',
  hues: [{ h: 285, s: 55 }],
  intensity: 45,
  bg: { kind: 'gradient' },
  grain: 12,
  dim: 0,
  blur: 0,
  parts: {},
};
