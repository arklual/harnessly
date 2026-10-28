// A deck: a design project of the kind `slides`, one board per slide in the order of its pages. These are the shapes the
// runtime's deck routes answer (`/design/deck/*`, internal/design/deck_types.go) and the page's own states the deck's
// screens share. No React here.
import type { ReactNode } from 'react';
import type { DesignDeckTone } from '@/next/protocol/v2';

export const DECK_TONE = { talk: 'talk', biz: 'biz', short: 'short' } as const satisfies Record<
  DesignDeckTone,
  DesignDeckTone
>;
export type TDeckTone = DesignDeckTone;

export const DECK_FONT = { inter: 'inter', golos: 'golos', serif: 'serif' } as const;
export type TDeckFont = (typeof DECK_FONT)[keyof typeof DECK_FONT];
/** The CSS stack of each font id, as the runtime writes it into deck-theme.css. */
export const DECK_FONT_CSS = {
  inter: 'Inter, system-ui, sans-serif',
  golos: '"Golos Text", Inter, sans-serif',
  serif: 'Georgia, "PT Serif", serif',
} as const satisfies Record<TDeckFont, string>;

export const DECK_SLIDES_MIN = 2;
export const DECK_SLIDES_MAX = 40;
export const DECK_MINUTES_CHOICES = [5, 10, 15, 20, 30, 45] as const;
/** Words a person says in a minute: the settings' «≈ {words} слов текста» and the agent's notes length. */
export const DECK_WORDS_PER_MINUTE = 130;

export interface IDeckTheme {
  id: string;
  name: string;
  /** `--deck-*` CSS variables. */
  vars: Record<string, string>;
}

export interface IDeckStyle {
  /** The template's file name; '' when there was none. */
  source: string;
  system: string | null;
  layouts: number;
}

/** meta/deck.json. */
export interface IDeckSettings {
  v: number;
  minutes: number;
  /** null: the agent picks the count. */
  slides: number | null;
  tone: TDeckTone;
  script: boolean;
  style: IDeckStyle | null;
  theme: string;
  font: TDeckFont;
  /** Absent: the built-in themes (Компания, Фиолет, Лес). */
  themes?: IDeckTheme[];
}

export interface IDeckSlide {
  /** The page's id: the key of its notes and comments. */
  id: string;
  /** The board's path in the project. */
  path: string;
  title: string;
  w: number;
  h: number;
  notes: string;
  /** The notes came from a dictation. */
  voice: boolean;
  seconds: number;
  /** Moves with the board's file: the key of the slide's picture. '' when the file cannot be read. */
  version: string;
  /** The board has nothing drawn on it yet: the agent made the page but not the slide. */
  blank: boolean;
}

export interface IDeckPerson {
  name: string;
  email: string;
}

/** A team comment, pinned to a point of a slide. */
export interface IDeckComment {
  id: string;
  page: string;
  /** Percent of the slide's width and height, 0..100. */
  x: number;
  y: number;
  /** The block under the point; '' when none. */
  label: string;
  text: string;
  by: IDeckPerson;
  at: string;
  resolved: boolean;
  resolvedBy: string | null;
  resolvedAt: string | null;
}

/** Where the id of a guest comment (kept by the gateway) starts in a resolve. */
export const DECK_GUEST_PREFIX = 'g-';

export const DECK_REC_STATE = { recording: 'recording', done: 'done', failed: 'failed' } as const;
export type TDeckRecState = (typeof DECK_REC_STATE)[keyof typeof DECK_REC_STATE];

export interface IDeckTake {
  page: string;
  n: number;
  startMs: number;
  endMs: number;
}

/** .media/rec/<id>/rec.json. */
export interface IDeckRecMeta {
  id: string;
  startedAt: string;
  durationMs: number;
  camera: boolean;
  mime: string;
  slides: string[];
  takes: IDeckTake[];
  state: TDeckRecState;
}

export const DECK_PART = { word: 'word', filler: 'filler', pause: 'pause' } as const;
export type TDeckPartKind = (typeof DECK_PART)[keyof typeof DECK_PART];

export interface IDeckPart {
  text: string;
  cut: boolean;
  kind: TDeckPartKind;
  t0: number;
  t1: number;
}

/** A recognised phrase; times are ms from the recording's start. */
export interface IDeckSegment {
  t0: number;
  t1: number;
  page: string;
  parts: IDeckPart[];
}

export const DECK_CAM = { br: 'br', bl: 'bl', tr: 'tr', tl: 'tl', off: 'off' } as const;
export type TDeckCam = (typeof DECK_CAM)[keyof typeof DECK_CAM];
export const DECK_CURSOR = { smooth: 'smooth', off: 'off' } as const;
export type TDeckCursor = (typeof DECK_CURSOR)[keyof typeof DECK_CURSOR];

/** The montage. `kept` are `<segment>.<part>` indexes kept although the cut would drop them. */
export interface IDeckEdit {
  cut: boolean;
  subs: boolean;
  kept: string[];
  cursor: TDeckCursor;
  cam: TDeckCam;
}

/** GET /design/deck/rec. `retaken` are 1-based slide numbers. */
export interface IDeckRecording {
  summary: IDeckRecMeta;
  segments: IDeckSegment[];
  edit: IDeckEdit;
  cutMs: number;
  retaken: number[];
}

export interface IDeckRecordingInfo {
  id: string;
  at: string;
  durationMs: number;
  state: TDeckRecState;
  retaken: number[];
}

/** A slide's start in the published video; `t` is ms. */
export interface IDeckChapter {
  page: string;
  title: string;
  t: number;
}

export interface IDeckPublished {
  video: boolean;
  pdf: boolean;
  /** The PDF is kept up to date for guests: the runtime builds it again when the deck changes. */
  pdfAuto: boolean;
  /** A newer PDF is being built (or is about to be: the deck changed since the published one). */
  pdfBuilding: boolean;
  /** When the published PDF was built from (ISO); null without one. */
  pdfAt: string | null;
  /** Why the last build failed, in the runtime's words; null when it did not. */
  pdfError: string | null;
}

/** GET /design/deck. `rev` moves whenever a deck file changes. */
export interface IDeckState {
  rev: number;
  settings: IDeckSettings;
  /** The machine can run the exports that need a headless Chrome / ffmpeg. */
  chrome: boolean;
  ffmpeg: boolean;
  slides: IDeckSlide[];
  comments: IDeckComment[];
  /** Guest comments marked resolved, as `g-<id>`. */
  guestResolved: string[];
  recordings: IDeckRecordingInfo[];
  published: IDeckPublished;
}

export const DECK_EXPORT = { pdf: 'pdf', pptx: 'pptx', mp4: 'mp4', png: 'png', html: 'html' } as const;
export type TDeckExport = (typeof DECK_EXPORT)[keyof typeof DECK_EXPORT];
/** The «Ещё» menu's order. */
export const DECK_EXPORTS: readonly TDeckExport[] = ['pdf', 'pptx', 'mp4', 'png', 'html'];

export const DECK_JOB = { queued: 'queued', running: 'running', done: 'done', failed: 'failed' } as const;
export type TDeckJobState = (typeof DECK_JOB)[keyof typeof DECK_JOB];

export interface IDeckExportJob {
  id: string;
  format: TDeckExport;
  state: TDeckJobState;
  /** 0..1. */
  progress: number;
  file: string | null;
  /** Worded for the person. */
  error: string | null;
  at: string;
}

export interface IDeckPptxStyle {
  name: string;
  colors: string[];
  fonts: { head: string; body: string };
  layouts: number;
  vars: Record<string, string>;
}

export interface IDeckTidied {
  id: string;
  notes: string;
  seconds: number;
  voice: boolean;
}

export const DECK_PATCH_OP = { text: 'text', style: 'style', move: 'move', reset: 'reset' } as const;
export type TDeckPatchOp = (typeof DECK_PATCH_OP)[keyof typeof DECK_PATCH_OP];

/**
 * A hand edit written into the board's source; `tpl` is the element's `data-dc-tpl`. A text op's `was` is the text
 * the person saw; a style, move or reset op's `was` is the element's fingerprint from the board's hover, select or
 * drag message. A mismatch is refused as `changed` unless exactly one element matches.
 */
export type TDeckPatch =
  | { op: 'text'; tpl: number; was: string; html: string }
  /** null removes the property. */
  | { op: 'style'; tpl: number; was: string; style: Record<string, string | null> }
  /** Board pixels, written as `translate` in cqw; in px when the board says the block sits in a container (`px`). */
  | { op: 'move'; tpl: number; was: string; x: number; y: number; unit?: 'px' }
  | { op: 'reset'; tpl: number; was: string };

/** What a refused patch says: the source changed under the page, or the element is interpolated. */
export const DECK_PATCH_REFUSAL = { changed: 'changed', dynamic: 'dynamic' } as const;
export type TDeckPatchRefusal = (typeof DECK_PATCH_REFUSAL)[keyof typeof DECK_PATCH_REFUSAL];

/** A line of events.jsonl; `t` is ms from the recording's start, board pixels for x and y. */
export type TDeckEvent =
  | { t: number; k: 'slide'; page: string; i: number }
  | { t: number; k: 'step'; n: number }
  | { t: number; k: 'cursor'; x: number; y: number }
  | { t: number; k: 'click'; x: number; y: number }
  | { t: number; k: 'laser'; on: boolean }
  | { t: number; k: 'pause' }
  | { t: number; k: 'resume' }
  | { t: number; k: 'retake'; page: string }
  /** A final of the recognition; `t0` is the time of its first partial. */
  | { t: number; k: 'asr'; t0: number; text: string }
  | { t: number; k: 'cam'; x: number; y: number; r: number };

// ---- the page's own states

/** The deck's screens, the shell's `data-screen`. */
export const DECK_SCREEN = {
  create: 'create',
  edit: 'edit',
  show: 'show',
  rec: 'rec',
  done: 'done',
  cut: 'cut',
  link: 'link',
} as const;
export type TDeckScreen = (typeof DECK_SCREEN)[keyof typeof DECK_SCREEN];

/** The left panel: docked, over the canvas, hidden (`data-panel`). */
export const DECK_PANEL = { dock: 'dock', float: 'float', off: 'off' } as const;
export type TDeckPanelMode = (typeof DECK_PANEL)[keyof typeof DECK_PANEL];

export const DECK_TAB = { chat: 'chat', notes: 'notes', comments: 'comments' } as const;
export type TDeckTab = (typeof DECK_TAB)[keyof typeof DECK_TAB];

/** «Как откроется» of a guest link, and the guest's mode (`data-lmode`). */
export const DECK_OPENS = { slides: 'slides', full: 'full', video: 'video' } as const;
export type TDeckOpens = (typeof DECK_OPENS)[keyof typeof DECK_OPENS];

/** «Кто откроет». */
export const DECK_AUDIENCE = { team: 'team', link: 'link', me: 'me' } as const;
export type TDeckAudience = (typeof DECK_AUDIENCE)[keyof typeof DECK_AUDIENCE];

/** The deck's flags in a guest link's `view` (the gateway keeps it as is). */
export interface IDeckLinkView {
  deck: TDeckOpens;
  pdf: boolean;
  stats: boolean;
}

/** The slide layouts the «Стиль» popover offers. */
export const DECK_LAYOUT = { cover: 'cover', statement: 'statement', list: 'list', cards: 'cards' } as const;
export type TDeckLayout = (typeof DECK_LAYOUT)[keyof typeof DECK_LAYOUT];

/** What the chat is about: the slide (1-based) and the block picked on it. */
export interface IDeckContext {
  slide: number;
  label: string | null;
}

/** The left panel's props (WP3's shell renders WP4's panel with these). */
export interface IDeckPanelProps {
  mode: TDeckPanelMode;
  tab: TDeckTab;
  onMode: (mode: TDeckPanelMode) => void;
  onTab: (tab: TDeckTab) => void;
  /** The site chat's node (the stage's `chat`): the Чат tab hosts it. */
  stage: ReactNode;
  /** 0-based. */
  slideIndex: number;
  onGo: (index: number) => void;
  ctx: IDeckContext | null;
  /** ≤560 px: the panel is a bottom sheet. */
  phone: boolean;
}
