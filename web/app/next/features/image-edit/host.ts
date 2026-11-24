// What the picture editor (image-editor.tsx) needs from where it is opened: a chat keeps its pictures on the machine and
// changes them through `<platform>/images`, a Sinopia design keeps them in the project and writes the chosen one into
// the board. The editor knows neither.

import type { MachineId } from '@/next/protocol/v2';

/** The generators a picture may be drawn with; which of them a machine offers, it says (providers.ts). */
export const IMAGE_PROVIDERS = ['codex', 'antigravity', 'higgsfield'] as const;
export type ImageProvider = (typeof IMAGE_PROVIDERS)[number];
export const isImageProvider = (v: unknown): v is ImageProvider =>
  typeof v === 'string' && (IMAGE_PROVIDERS as readonly string[]).includes(v);
const PROVIDER_NAME: Record<ImageProvider, string> = {
  codex: 'Codex',
  antigravity: 'Antigravity',
  higgsfield: 'Higgsfield',
};
export const imageProviderName = (provider: ImageProvider): string => PROVIDER_NAME[provider];
/** Only Codex draws a transparent background and removes one. */
export const drawsTransparent = (provider: ImageProvider): boolean => provider === 'codex';

export const EDIT_MODE = {
  text: 'text',
  mark: 'mark',
  note: 'note',
  erase: 'erase',
  cut: 'cut',
  size: 'size',
} as const;
export type TEditMode = (typeof EDIT_MODE)[keyof typeof EDIT_MODE];

export const EDIT_FORMATS = ['1:1', '4:3', '3:4', '16:9', '9:16'] as const;
export type TEditFormat = (typeof EDIT_FORMATS)[number];

/** One state of a picture: the original or the result of a change. */
export interface IImageVersion {
  /** The generator reported by the job; absent for original or older pictures. */
  provider?: ImageProvider;
  /** What the host names this version by when it is changed further (a file path, an asset of the project). */
  id: string;
  /** Where the bytes are, for an <img>. */
  src: string;
  /** Has real transparency: shown on a checkerboard. */
  clear?: boolean;
}

export interface IImageEdit {
  /** Absent on older saved runs: Codex. */
  provider?: ImageProvider;
  /** The provider's model (Higgsfield's), absent for its default. */
  model?: string;
  from: IImageVersion;
  mode: TEditMode;
  /** The person's words, '' when the mode needs none. */
  prompt: string;
  /** For the mode `size`. */
  format?: TEditFormat;
  /** What the person drew, a base64 PNG (no data: prefix): opaque magenta for `erase` and `note`, the chosen colours for `mark`. */
  overlay?: string;
}

/** A change being made, as the host last heard. */
export interface IEditProgress {
  state: 'running' | 'done' | 'failed';
  /** The new versions, when done. */
  versions: readonly IImageVersion[];
  error?: string;
}

export interface IImageEditHost {
  /** The machine the pictures are changed on: it says which generators it has and what a paid one costs. */
  machine?: MachineId;
  /** Starts a change and answers what to follow it by; rejects with an Error the person may read. */
  start: (edit: IImageEdit) => Promise<string>;
  read: (run: string, signal: AbortSignal) => Promise<IEditProgress>;
  /** The versions known so far, the original first; a chat reads them from the machine, so they outlive a reload. */
  versions: (original: IImageVersion, signal: AbortSignal) => Promise<readonly IImageVersion[]>;
  /**
   * The viewer opens on the newest version: a chat's picture stays what it was, its changes are beside it. A design's
   * element shows the chosen version, which is the one to open on.
   */
  opensLatest?: boolean;
  /** The person chose a version (a new one is chosen when it arrives). A design writes it into the element. */
  chosen?: (version: IImageVersion) => void;
}
