// The design's picture routes (`<platform>/design/image`, internal/design/deckimage.go) and the job's state
// (`<platform>/images?id=`): a picture is drawn into the project's assets/ and written into a board by the patch op
// `image`. What the machine says is read here, field by field; an answer that is not what was asked is an error.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { RuntimeError, postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

export const GEN_SHAPE = { square: 'square', landscape: 'landscape', portrait: 'portrait' } as const;
export type TGenShape = (typeof GEN_SHAPE)[keyof typeof GEN_SHAPE];

/** The most variants one request draws (the route's own limit). */
export const GEN_MAX_VARIANTS = 4;
export const GEN_VARIANTS = [1, 2, 3, 4] as const;

export const JOB_STATE = { running: 'running', done: 'done', failed: 'failed' } as const;
export type TJobState = (typeof JOB_STATE)[keyof typeof JOB_STATE];

/** One picture drawn: its place in the project (`assets/<job>-<n>.png`), which is what the board references. */
export interface IGenPic {
  n: number;
  src: string;
}

export interface IGenJob {
  id: string;
  state: TJobState;
  pics: IGenPic[];
  error: string | null;
}

export interface IGenStarted {
  /** The name of the job's files; the element drawn from it remembers it (`data-gen-job`). */
  name: string;
  job: IGenJob;
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;
const isState = (v: unknown): v is TJobState => v === 'running' || v === 'done' || v === 'failed';

const baseName = (path: string) => path.slice(path.lastIndexOf('/') + 1);

const readJob = (v: unknown, name: string): IGenJob | null => {
  if (!isRecord(v) || typeof v.id !== 'string' || !isState(v.state) || !Array.isArray(v.images)) return null;
  const pics: IGenPic[] = [];
  for (const image of v.images) {
    if (!isRecord(image) || typeof image.n !== 'number') return null;
    // A picture with no copy in the project cannot be written into a board.
    if (typeof image.copy !== 'string' || image.copy === '') continue;
    pics.push({ n: image.n, src: `assets/${baseName(image.copy)}` });
  }
  if (pics.some((pic) => !pic.src.startsWith(`assets/${name}-`))) return null;
  return {
    id: v.id,
    state: v.state,
    pics,
    error: typeof v.error === 'string' && v.error !== '' ? v.error : null,
  };
};

const must = <T>(v: T | null): T => {
  if (v === null) throw new RuntimeError(t('next.failure.http'));
  return v;
};

export interface IGenRequest {
  artifact: string;
  prompt: string;
  count: number;
  shape: TGenShape;
}

export const startDesignImage = async (m: MachineId, request: IGenRequest): Promise<IGenStarted> => {
  const body = await runtimeJson<unknown>(m, platformPath('/design/image'), postJson(request));
  const name = isRecord(body) && typeof body.name === 'string' ? body.name : null;
  if (name === null) throw new RuntimeError(t('next.failure.http'));
  return { name, job: must(readJob(isRecord(body) ? body.job : null, name)) };
};

export interface IEditRequest {
  artifact: string;
  /** The picture to change, a path of the project. */
  source: string;
  mode: string;
  prompt: string;
  format?: string;
  /** What the person drew, a base64 PNG. */
  overlay?: string;
}

/** Starts a change of a picture of the project (the editor's); one picture comes back, as a new file of the project. */
export const startDesignEdit = async (m: MachineId, request: IEditRequest): Promise<IGenStarted> => {
  const body = await runtimeJson<unknown>(
    m,
    platformPath('/design/image'),
    postJson({ ...request, count: 1, shape: GEN_SHAPE.square }),
  );
  const name = isRecord(body) && typeof body.name === 'string' ? body.name : null;
  if (name === null) throw new RuntimeError(t('next.failure.http'));
  return { name, job: must(readJob(isRecord(body) ? body.job : null, name)) };
};

export const readGenJob = async (
  m: MachineId,
  id: string,
  name: string,
  signal?: AbortSignal,
): Promise<IGenJob> =>
  must(
    readJob(
      await runtimeJson<unknown>(m, platformPath(`/images?id=${encodeURIComponent(id)}`), {
        signal,
        cache: 'no-store',
      }),
      name,
    ),
  );

/** The pictures the project holds of a job (the variants of an element drawn earlier), also after the job is forgotten. */
export const readGenVariants = async (
  m: MachineId,
  artifact: string,
  name: string,
  signal?: AbortSignal,
): Promise<IGenPic[]> => {
  const q = `artifact=${encodeURIComponent(artifact)}&job=${encodeURIComponent(name)}`;
  const body = await runtimeJson<unknown>(m, platformPath(`/design/image?${q}`), {
    signal,
    cache: 'no-store',
  });
  const list = isRecord(body) ? body.images : null;
  if (!Array.isArray(list)) throw new RuntimeError(t('next.failure.http'));
  return list.map((item) => {
    if (!isRecord(item) || typeof item.n !== 'number' || typeof item.src !== 'string') {
      throw new RuntimeError(t('next.failure.http'));
    }
    return { n: item.n, src: item.src };
  });
};

/** The frame a picture should be drawn in, from the element's width over its height. */
export const shapeOf = (ratio: number): TGenShape => {
  if (ratio >= 1.25) return GEN_SHAPE.landscape;
  if (ratio > 0 && ratio <= 0.8) return GEN_SHAPE.portrait;
  return GEN_SHAPE.square;
};

/** A picture of the project for a thumbnail: the route serves only a job's own files from the project's assets. */
export const thumbUrl = (m: MachineId, artifact: string, src: string) =>
  runtimePath(
    platformPath(
      `/design/image/file?artifact=${encodeURIComponent(artifact)}&file=${encodeURIComponent(baseName(src))}`,
    ),
    m,
  );

/** Any picture of the project by its path in it, for the editor: the route confines the path to the project. */
export const projectPictureUrl = (m: MachineId, artifact: string, path: string) =>
  runtimePath(
    platformPath(
      `/design/image/file?artifact=${encodeURIComponent(artifact)}&path=${encodeURIComponent(path)}`,
    ),
    m,
  );

/** The board an element is in: a deck's slide (its page id) or a board named by its file. */
export type TBoardAddress = string | { file: string };

/**
 * The project's own picture of an element, from the board's source: the page sees a heavy file by its content address
 * only, so the path of the file (`assets/a.jpg`) is asked of the machine, against the fingerprint the page gave.
 */
export const readDesignSource = async (
  m: MachineId,
  artifact: string,
  address: TBoardAddress,
  tpl: number,
  was: string,
  signal?: AbortSignal,
): Promise<string> => {
  const board =
    typeof address === 'string'
      ? `page=${encodeURIComponent(address)}`
      : `file=${encodeURIComponent(address.file)}`;
  const q = `artifact=${encodeURIComponent(artifact)}&${board}&tpl=${tpl}&was=${encodeURIComponent(was)}`;
  const body = await runtimeJson<unknown>(m, platformPath(`/design/image?${q}`), {
    signal,
    cache: 'no-store',
  });
  const src = isRecord(body) ? body.src : null;
  if (typeof src !== 'string' || src === '') throw new RuntimeError(t('next.failure.http'));
  return src;
};
