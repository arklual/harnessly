// The picture card's data: what the machine says of a harnessly picture job while it is being drawn
// (GET <platform>/images?conv=…), and what the card shows from it and from the entry (see image-card.tsx). Codex's own
// image tool has no job: its pictures arrive in the entry itself.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { ConvId, ImageGen, MachineId } from '@/next/protocol/v2';

export const JOB_STATES = ['running', 'done', 'failed'] as const;
export type JobState = (typeof JOB_STATES)[number];

export interface IImageJob {
  id: string;
  prompt: string;
  count: number;
  state: JobState;
  /** The files drawn so far (the copy in the project when one was asked for, else harnessly's own). */
  paths: string[];
  error: string | null;
  startedAt: number;
  /** For a change of a picture: the picture it changes and how (absent for a new picture). */
  source?: string;
  mode?: string;
}

/** How often a running job is asked about. */
export const JOB_POLL_MS = 1500;
/** A job started this long before its call was logged still belongs to it (the two clocks are the machine's own). */
const JOB_SLACK_MS = 10_000;

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;
const isState = (v: unknown): v is JobState => JOB_STATES.some((state) => state === v);

const pathOf = (v: unknown): string | null => {
  if (!isRecord(v)) return null;
  const copy = typeof v.copy === 'string' ? v.copy : '';
  if (copy !== '') return copy;
  return typeof v.path === 'string' && v.path !== '' ? v.path : null;
};

const decodeJob = (v: unknown): IImageJob | null => {
  if (!isRecord(v)) return null;
  const { id, prompt, count, state, images, error, startedAt } = v;
  if (typeof id !== 'string' || typeof prompt !== 'string' || typeof count !== 'number') return null;
  if (!isState(state) || !Array.isArray(images) || typeof startedAt !== 'number') return null;
  const paths = images.map(pathOf).filter((path): path is string => path !== null);
  const job: IImageJob = {
    id,
    prompt,
    count,
    state,
    paths,
    error: typeof error === 'string' ? error : null,
    startedAt,
  };
  if (typeof v.source === 'string' && v.source !== '') job.source = v.source;
  if (typeof v.mode === 'string' && v.mode !== '') job.mode = v.mode;
  return job;
};

/** The jobs of a chat as the machine lists them. A malformed answer is an error, never an empty list. */
export const listJobs = async (
  machine: MachineId,
  conv: ConvId,
  signal: AbortSignal,
): Promise<IImageJob[]> => {
  const url = runtimePath(platformPath(`/images?conv=${encodeURIComponent(conv)}`), machine);
  const res = await fetch(url, { credentials: 'same-origin', signal });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const doc: unknown = await res.json();
  const list = isRecord(doc) ? doc.jobs : null;
  if (!Array.isArray(list)) throw new Error('bad answer');
  return list.map(decodeJob).filter((job): job is IImageJob => job !== null);
};

/** The job a call is drawing: by its job id once the call knows it, else the first job with its brief started after it. */
export const jobOf = (jobs: readonly IImageJob[], images: ImageGen, at: number): IImageJob | null => {
  if (images.jobId !== '') return jobs.find((job) => job.id === images.jobId) ?? null;
  const brief = images.prompt.trim();
  const mine = jobs.filter((job) => job.prompt.trim() === brief && job.startedAt >= at - JOB_SLACK_MS);
  return mine.sort((a, b) => a.startedAt - b.startedAt)[0] ?? null;
};

/** The names of the harnessly picture tools: the card follows their jobs (Codex's own tool has none). */
export const followsJob = (tool: string): boolean => /(^|__)(generate|wait)_images$/.test(tool);

export interface ICardView {
  /** One per picture: its file, or null while it is being drawn. */
  tiles: (string | null)[];
  drawing: boolean;
  failed: string | null;
}

/** What the card shows: the entry's pictures, or the job's when the machine knows more than the transcript yet. */
export const cardView = (images: ImageGen, live: IImageJob | null): ICardView => {
  const fresh = live !== null && live.paths.length > images.done.length;
  const done = fresh ? live.paths : images.done;
  const settled = live !== null && live.state !== 'running';
  const drawing = images.state === 'drawing' && !settled;
  const count = Math.max(images.count, live?.count ?? 0, done.length);
  const tiles = drawing ? Array.from({ length: count }, (_, i) => done[i] ?? null) : [...done];
  let failed = images.failed;
  if (failed === null && live?.state === 'failed') failed = live.error;
  return { tiles, drawing, failed };
};

/** Columns of the grid by the number of tiles: one big, two side by side, four as 2×2, more in threes. */
export const columnsOf = (tiles: number): 1 | 2 | 3 => {
  if (tiles <= 1) return 1;
  if (tiles <= 4) return 2;
  return 3;
};

export const SHAPES = ['square', 'landscape', 'portrait'] as const;
export type Shape = (typeof SHAPES)[number];

/** The frame a call asked for; null when it did not say (Codex's own tool). */
export const shapeOf = (shape: string): Shape | null => SHAPES.find((known) => known === shape) ?? null;

/** The frame of a loaded picture, by its own proportions. */
export const shapeOfSize = (width: number, height: number): Shape => {
  const ratio = width / height;
  if (ratio >= 1.2) return 'landscape';
  if (ratio <= 0.83) return 'portrait';
  return 'square';
};
