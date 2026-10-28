// The deck's runtime routes (`<platform>/design/deck/*`, cmd/runtime/routes_design_deck.go): every call the deck's
// screens make, answered as read by ./wire. A refusal in the runtime's words is a thrown RuntimeError; the two refusals
// a screen acts on (a patch the source moved under, a chunk out of order) come back as values instead.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { RuntimeError, postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import {
  readDeckComment,
  readDeckExportJob,
  readDeckPptxStyle,
  readDeckPdfAnswer,
  readDeckRecording,
  readDeckSettings,
  readDeckSlide,
  readDeckState,
  readDeckTidied,
} from '@/next/features/designly/deck/wire';
import type {
  IDeckComment,
  IDeckEdit,
  IDeckExportJob,
  IDeckPptxStyle,
  IDeckPublished,
  IDeckRecording,
  IDeckSettings,
  IDeckSlide,
  IDeckState,
  IDeckTidied,
  TDeckEvent,
  TDeckExport,
  TDeckPatch,
  TDeckPatchRefusal,
} from '@/next/features/designly/deck/types';

type TPath = `/${string}`;

const deck = (path: string): TPath => platformPath(`/design/deck${path}` as TPath);
const q = (params: Record<string, string | number>) =>
  Object.entries(params)
    .map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`)
    .join('&');

const jsonInit = (method: 'PUT' | 'PATCH', body: unknown): RequestInit => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
});

/** An answer the reader could not take is the runtime not speaking the deck's language (an older one). */
const must = <T>(v: T | null): T => {
  if (v === null) throw new RuntimeError(t('next.failure.http'));
  return v;
};

// ---- the deck, settings, notes, comments (WP1's routes)

export const fetchDeck = async (m: MachineId, artifact: string, signal?: AbortSignal): Promise<IDeckState> =>
  must(readDeckState(await runtimeJson(m, deck(`?${q({ artifact })}`), { signal, cache: 'no-store' })));

export const putDeckSettings = async (
  m: MachineId,
  artifact: string,
  settings: Partial<IDeckSettings>,
): Promise<IDeckSettings> => {
  const answer = await runtimeJson<{ settings: unknown }>(
    m,
    deck('/settings'),
    jsonInit('PUT', { artifact, settings }),
  );
  return readDeckSettings(answer.settings);
};

export const putDeckNotes = async (
  m: MachineId,
  artifact: string,
  page: string,
  text: string,
): Promise<IDeckSlide> => {
  const answer = await runtimeJson<{ slide: unknown }>(
    m,
    deck('/notes'),
    jsonInit('PUT', { artifact, page, text }),
  );
  return must(readDeckSlide(answer.slide));
};

/** A dictation made into notes; `page` null is the whole deck, which the agent splits by slide. */
export const tidyDeckNotes = async (
  m: MachineId,
  artifact: string,
  page: string | null,
  raw: string,
  lang: string,
): Promise<IDeckTidied[]> => {
  const answer = await runtimeJson<{ slides: unknown }>(
    m,
    deck('/tidy'),
    postJson({ artifact, page, raw, lang }),
  );
  return readDeckTidied(answer.slides);
};

export interface IDeckCommentDraft {
  page: string;
  x: number;
  y: number;
  label: string;
  text: string;
}

export const addDeckComment = async (
  m: MachineId,
  artifact: string,
  draft: IDeckCommentDraft,
): Promise<IDeckComment> =>
  must(readDeckComment(await runtimeJson(m, deck('/comments'), postJson({ artifact, ...draft }))));

/** `id` may be a guest comment's `g-<id>`: the answer then is only `{id, resolved}`. */
export const resolveDeckComment = async (
  m: MachineId,
  artifact: string,
  id: string,
  resolved: boolean,
): Promise<{ id: string; resolved: boolean; comment: IDeckComment | null }> => {
  const answer = await runtimeJson<Record<string, unknown>>(
    m,
    deck('/comments'),
    jsonInit('PATCH', { artifact, id, resolved }),
  );
  return { id, resolved: answer.resolved === true, comment: readDeckComment(answer) };
};

/** A company template's look; `null` when the file is not a template the runtime can read. */
export const readPptxStyle = async (m: MachineId, file: Blob): Promise<IDeckPptxStyle | null> => {
  const res = await fetch(runtimePath(deck('/pptx-style'), m), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/vnd.openxmlformats-officedocument.presentationml.presentation' },
    body: file,
  });
  if (!res.ok) return null;
  return readDeckPptxStyle(await res.json().catch(() => null));
};

// ---- hand edits (WP2's route)

export type TDeckPatchResult = { ok: true; rev: number } | { ok: false; code: TDeckPatchRefusal };

export const patchDeck = async (
  m: MachineId,
  artifact: string,
  page: string,
  patch: TDeckPatch,
): Promise<TDeckPatchResult> => {
  const res = await fetch(runtimePath(deck('/patch'), m), {
    credentials: 'same-origin',
    ...postJson({ artifact, page, ...patch }),
  });
  const body: unknown = await res.json().catch(() => null);
  const r = (body && typeof body === 'object' ? body : {}) as Record<string, unknown>;
  if (res.ok) return { ok: true, rev: typeof r.rev === 'number' ? r.rev : 0 };
  if ((res.status === 409 && r.code === 'changed') || (res.status === 422 && r.code === 'dynamic')) {
    return { ok: false, code: r.code as TDeckPatchRefusal };
  }
  throw new RuntimeError(typeof r.error === 'string' && r.error !== '' ? r.error : t('next.failure.http'));
};

// ---- recordings (WP6's routes)

export const startDeckRec = async (
  m: MachineId,
  artifact: string,
  camera: boolean,
  mime: string,
): Promise<string> => {
  const answer = await runtimeJson<{ rec: unknown }>(m, deck('/rec'), postJson({ artifact, camera, mime }));
  return must(typeof answer.rec === 'string' && answer.rec !== '' ? answer.rec : null);
};

export type TDeckChunkResult = { ok: true } | { ok: false; next: number };

/** One chunk of the recording; the same `seq` again is a no-op, a gap answers with the `next` the runtime wants. */
export const sendDeckChunk = async (
  m: MachineId,
  artifact: string,
  rec: string,
  seq: number,
  chunk: Blob,
): Promise<TDeckChunkResult> => {
  const res = await fetch(runtimePath(deck(`/rec/chunk?${q({ artifact, rec, seq })}`), m), {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': chunk.type || 'application/octet-stream' },
    body: chunk,
  });
  if (res.ok) return { ok: true };
  const body: unknown = await res.json().catch(() => null);
  const next = body && typeof body === 'object' ? (body as Record<string, unknown>).next : undefined;
  if (res.status === 409 && typeof next === 'number') return { ok: false, next };
  throw new RuntimeError(t('next.failure.http'));
};

export const sendDeckEvents = (m: MachineId, artifact: string, rec: string, lines: TDeckEvent[]) =>
  runtimeJson<unknown>(m, deck('/rec/events'), postJson({ artifact, rec, lines })).then(() => undefined);

export const finishDeckRec = async (
  m: MachineId,
  artifact: string,
  rec: string,
  durationMs: number,
): Promise<IDeckRecording> =>
  must(readDeckRecording(await runtimeJson(m, deck('/rec/finish'), postJson({ artifact, rec, durationMs }))));

export const fetchDeckRec = async (
  m: MachineId,
  artifact: string,
  rec: string,
  signal?: AbortSignal,
): Promise<IDeckRecording> =>
  must(
    readDeckRecording(
      await runtimeJson(m, deck(`/rec?${q({ artifact, rec })}`), { signal, cache: 'no-store' }),
    ),
  );

export const putDeckEdit = async (
  m: MachineId,
  artifact: string,
  rec: string,
  edit: IDeckEdit,
): Promise<IDeckRecording> =>
  must(readDeckRecording(await runtimeJson(m, deck('/rec/edit'), jsonInit('PUT', { artifact, rec, edit }))));

/** The recording's own video (camera and voice) for a `<video>`: served with Range. */
export const deckRecMediaHref = (m: MachineId, artifact: string, rec: string) =>
  runtimePath(deck(`/rec/media?${q({ artifact, rec })}`), m);

// ---- exports (WP7's routes)

export const startDeckExport = async (
  m: MachineId,
  artifact: string,
  format: TDeckExport,
  rec?: string,
  /** A PDF only: also keep the finished file as the one guests may download (needs the edit role). */
  publish?: boolean,
): Promise<IDeckExportJob> =>
  must(
    readDeckExportJob(await runtimeJson(m, deck('/export'), postJson({ artifact, format, rec, publish }))),
  );

export const fetchDeckExport = async (
  m: MachineId,
  artifact: string,
  job: string,
  signal?: AbortSignal,
): Promise<IDeckExportJob> =>
  must(
    readDeckExportJob(
      await runtimeJson(m, deck(`/export?${q({ artifact, job })}`), { signal, cache: 'no-store' }),
    ),
  );

/**
 * Starts or stops keeping the deck's PDF for guests (needs the edit role). It answers at once: the runtime builds the
 * PDF in the background and again whenever the deck changes, and publishes each build for the guests.
 */
export const putDeckPdfAuto = async (
  m: MachineId,
  artifact: string,
  auto: boolean,
): Promise<IDeckPublished> =>
  must(readDeckPdfAnswer(await runtimeJson(m, deck('/pdf'), postJson({ artifact, auto }))));

/** Where a finished export downloads from (an attachment named after the deck). */
export const deckExportHref = (m: MachineId, artifact: string, job: string) =>
  runtimePath(deck(`/export/file?${q({ artifact, job })}`), m);
