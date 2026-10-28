// The deck routes' answers read as data: every field is checked, a Go slice that was empty may come as null and reads
// as [], an unknown enum value falls back to the deck's default, and a malformed item of a list is left out. The
// readers never throw; a top level that is not an object reads as null. No React here.
import {
  DECK_CAM,
  DECK_CURSOR,
  DECK_EXPORT,
  DECK_FONT,
  DECK_JOB,
  DECK_PART,
  DECK_REC_STATE,
  DECK_TONE,
  type IDeckChapter,
  type IDeckComment,
  type IDeckEdit,
  type IDeckExportJob,
  type IDeckPptxStyle,
  type IDeckPublished,
  type IDeckRecMeta,
  type IDeckRecording,
  type IDeckRecordingInfo,
  type IDeckSegment,
  type IDeckSettings,
  type IDeckSlide,
  type IDeckState,
  type IDeckStyle,
  type IDeckTheme,
  type IDeckTidied,
} from '@/next/features/designly/deck/types';

type TRecord = Record<string, unknown>;

const TEXT_MAX = 20_000;

const isRecord = (v: unknown): v is TRecord => !!v && typeof v === 'object' && !Array.isArray(v);
const num = (v: unknown, fallback = 0) => (typeof v === 'number' && Number.isFinite(v) ? v : fallback);
const text = (v: unknown, max = TEXT_MAX) => (typeof v === 'string' ? v.slice(0, max) : '');
const textOrNull = (v: unknown, max = TEXT_MAX) => (typeof v === 'string' ? v.slice(0, max) : null);
const bool = (v: unknown) => v === true;
const records = (v: unknown): TRecord[] => (Array.isArray(v) ? v.filter(isRecord) : []);
const strings = (v: unknown, max = 200): string[] =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string').map((x) => x.slice(0, max)) : [];
const ints = (v: unknown): number[] =>
  Array.isArray(v) ? v.filter((x): x is number => Number.isInteger(x)) : [];
const oneOf = <T extends string>(all: Record<string, T>, v: unknown, fallback: T): T =>
  typeof v === 'string' && (Object.values(all) as string[]).includes(v) ? (v as T) : fallback;
const vars = (v: unknown): Record<string, string> => {
  if (!isRecord(v)) return {};
  const out: Record<string, string> = {};
  for (const [k, x] of Object.entries(v)) {
    if (/^--deck-[a-z0-9-]{1,32}$/.test(k) && typeof x === 'string') out[k] = x.slice(0, 64);
  }
  return out;
};

const readTheme = (r: TRecord): IDeckTheme | null => {
  const id = text(r.id, 32);
  return id === '' ? null : { id, name: text(r.name, 80), vars: vars(r.vars) };
};

const readStyle = (v: unknown): IDeckStyle | null =>
  isRecord(v)
    ? { source: text(v.source, 200), system: textOrNull(v.system, 200), layouts: Math.max(0, num(v.layouts)) }
    : null;

export const readDeckSettings = (v: unknown): IDeckSettings => {
  const r = isRecord(v) ? v : {};
  const settings: IDeckSettings = {
    v: num(r.v, 1),
    minutes: num(r.minutes, 10),
    slides: typeof r.slides === 'number' && Number.isInteger(r.slides) ? r.slides : null,
    tone: oneOf(DECK_TONE, r.tone, DECK_TONE.talk),
    script: r.script !== false,
    style: readStyle(r.style),
    theme: text(r.theme, 32),
    font: oneOf(DECK_FONT, r.font, DECK_FONT.inter),
  };
  if (Array.isArray(r.themes)) {
    settings.themes = records(r.themes).flatMap((x) => readTheme(x) ?? []);
  }
  return settings;
};

export const readDeckSlide = (v: unknown): IDeckSlide | null => {
  if (!isRecord(v)) return null;
  const id = text(v.id, 64);
  if (id === '') return null;
  return {
    id,
    path: text(v.path, 400),
    title: text(v.title, 200),
    w: num(v.w),
    h: num(v.h),
    notes: text(v.notes),
    voice: bool(v.voice),
    seconds: num(v.seconds),
    version: text(v.version, 80),
    blank: bool(v.blank),
  };
};

export const readDeckComment = (v: unknown): IDeckComment | null => {
  if (!isRecord(v)) return null;
  const id = text(v.id, 64);
  if (id === '') return null;
  const by = isRecord(v.by) ? v.by : {};
  return {
    id,
    page: text(v.page, 64),
    x: Math.min(100, Math.max(0, num(v.x))),
    y: Math.min(100, Math.max(0, num(v.y))),
    label: text(v.label, 120),
    text: text(v.text, 4000),
    by: { name: text(by.name, 120), email: text(by.email, 200) },
    at: text(v.at, 40),
    resolved: bool(v.resolved),
    resolvedBy: textOrNull(v.resolvedBy, 200),
    resolvedAt: textOrNull(v.resolvedAt, 40),
  };
};

const readRecInfo = (r: TRecord): IDeckRecordingInfo | null => {
  const id = text(r.id, 64);
  if (id === '') return null;
  return {
    id,
    at: text(r.at, 40),
    durationMs: num(r.durationMs),
    state: oneOf(DECK_REC_STATE, r.state, DECK_REC_STATE.failed),
    retaken: ints(r.retaken),
  };
};

/** What guests may read of a deck (GET /design/deck's `published`, POST /design/deck/pdf's answer). */
export const readDeckPublished = (v: unknown): IDeckPublished => {
  const p = isRecord(v) ? v : {};
  return {
    video: bool(p.video),
    pdf: bool(p.pdf),
    pdfAuto: bool(p.pdfAuto),
    pdfBuilding: bool(p.pdfBuilding),
    pdfAt: textOrNull(p.pdfAt, 40),
    pdfError: textOrNull(p.pdfError),
  };
};

/** POST /design/deck/pdf: `{published}`; null when the answer is not that. */
export const readDeckPdfAnswer = (v: unknown): IDeckPublished | null =>
  isRecord(v) && isRecord(v.published) ? readDeckPublished(v.published) : null;

/** GET /design/deck; null when the answer is not a deck. */
export const readDeckState = (v: unknown): IDeckState | null => {
  if (!isRecord(v)) return null;
  return {
    rev: num(v.rev),
    settings: readDeckSettings(v.settings),
    chrome: bool(v.chrome),
    ffmpeg: bool(v.ffmpeg),
    slides: (Array.isArray(v.slides) ? v.slides : []).flatMap((x) => readDeckSlide(x) ?? []),
    comments: (Array.isArray(v.comments) ? v.comments : []).flatMap((x) => readDeckComment(x) ?? []),
    guestResolved: strings(v.guestResolved, 80),
    recordings: records(v.recordings).flatMap((x) => readRecInfo(x) ?? []),
    published: readDeckPublished(v.published),
  };
};

const readRecMeta = (v: unknown): IDeckRecMeta => {
  const r = isRecord(v) ? v : {};
  return {
    id: text(r.id, 64),
    startedAt: text(r.startedAt, 40),
    durationMs: num(r.durationMs),
    camera: bool(r.camera),
    mime: text(r.mime, 120),
    slides: strings(r.slides, 64),
    takes: records(r.takes).map((x) => ({
      page: text(x.page, 64),
      n: num(x.n),
      startMs: num(x.startMs),
      endMs: num(x.endMs),
    })),
    state: oneOf(DECK_REC_STATE, r.state, DECK_REC_STATE.failed),
  };
};

const readSegment = (r: TRecord): IDeckSegment => ({
  t0: num(r.t0),
  t1: num(r.t1),
  page: text(r.page, 64),
  parts: records(r.parts).map((p) => ({
    text: text(p.text, 4000),
    cut: bool(p.cut),
    kind: oneOf(DECK_PART, p.kind, DECK_PART.word),
    t0: num(p.t0),
    t1: num(p.t1),
  })),
});

export const readDeckEdit = (v: unknown): IDeckEdit => {
  const r = isRecord(v) ? v : {};
  return {
    cut: r.cut !== false,
    subs: r.subs !== false,
    kept: strings(r.kept, 24),
    cursor: oneOf(DECK_CURSOR, r.cursor, DECK_CURSOR.smooth),
    cam: oneOf(DECK_CAM, r.cam, DECK_CAM.br),
  };
};

/** GET/PUT /design/deck/rec, POST /design/deck/rec/finish; null when the answer is not a recording. */
export const readDeckRecording = (v: unknown): IDeckRecording | null => {
  if (!isRecord(v) || !isRecord(v.summary)) return null;
  return {
    summary: readRecMeta(v.summary),
    segments: records(v.segments).map(readSegment),
    edit: readDeckEdit(v.edit),
    cutMs: num(v.cutMs),
    retaken: ints(v.retaken),
  };
};

/** POST/GET /design/deck/export; null when the answer is not a job. */
export const readDeckExportJob = (v: unknown): IDeckExportJob | null => {
  if (!isRecord(v)) return null;
  const id = text(v.id, 64);
  const format = typeof v.format === 'string' ? v.format : '';
  if (id === '' || !(Object.values(DECK_EXPORT) as string[]).includes(format)) return null;
  return {
    id,
    format: format as IDeckExportJob['format'],
    state: oneOf(DECK_JOB, v.state, DECK_JOB.failed),
    progress: Math.min(1, Math.max(0, num(v.progress))),
    file: textOrNull(v.file, 200),
    error: textOrNull(v.error, 400),
    at: text(v.at, 40),
  };
};

/** POST /design/deck/pptx-style; null when the template could not be read. */
export const readDeckPptxStyle = (v: unknown): IDeckPptxStyle | null => {
  if (!isRecord(v)) return null;
  const fonts = isRecord(v.fonts) ? v.fonts : {};
  return {
    name: text(v.name, 200),
    colors: strings(v.colors, 32).slice(0, 12),
    fonts: { head: text(fonts.head, 80), body: text(fonts.body, 80) },
    layouts: Math.max(0, num(v.layouts)),
    vars: vars(v.vars),
  };
};

/** POST /design/deck/tidy's `slides`. */
export const readDeckTidied = (v: unknown): IDeckTidied[] =>
  records(v).flatMap((r) => {
    const id = text(r.id, 64);
    return id === '' ? [] : [{ id, notes: text(r.notes), seconds: num(r.seconds), voice: bool(r.voice) }];
  });

/** The published video's chapters (the public index's `deck.video.chapters`). */
export const readDeckChapters = (v: unknown): IDeckChapter[] =>
  records(v).map((r) => ({ page: text(r.page, 64), title: text(r.title, 200), t: num(r.t) }));
