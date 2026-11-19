// The 3D scene's runtime routes (`<platform>/design/scene3d/*`, cmd/runtime/routes_design_scene3d.go): every call the
// scene's screens make, answered as read by ./wire. A refusal in the runtime's words is a thrown RuntimeError.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { RuntimeError, postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import {
  readClip,
  readFrame,
  readPin,
  readScene3dState,
  readViewCmds,
} from '@/next/features/designly/scene3d/wire';
import type {
  IPinDraft,
  IScene3dCam,
  IScene3dClip,
  IScene3dFinal,
  IScene3dFinalPlan,
  IScene3dGeneration,
  IScene3dOutputs,
  IScene3dVideoResult,
  IScene3dFrame,
  IScene3dPin,
  IScene3dState,
  IScene3dViewCmd,
} from '@/next/features/designly/scene3d/types';

type TPath = `/${string}`;

const scene = (path: string): TPath => platformPath(`/design/scene3d${path}` as TPath);
const q = (params: Record<string, string | number>) =>
  Object.entries(params)
    .map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`)
    .join('&');

const must = <T>(v: T | null): T => {
  if (v === null) throw new RuntimeError(t('next.failure.http'));
  return v;
};

export const fetchScene3d = async (
  m: MachineId,
  artifact: string,
  signal?: AbortSignal,
): Promise<IScene3dState> =>
  must(
    readScene3dState(
      await runtimeJson<unknown>(m, scene(`?${q({ artifact })}`), { signal, cache: 'no-store' }),
    ),
  );

/** The model's address: by its hash it never changes, so the browser keeps it. */
export const modelUrl = (m: MachineId, artifact: string, hash: string): string =>
  runtimePath(scene(`/model?${q({ artifact, h: hash })}`), m);

export const frameUrl = (m: MachineId, artifact: string, key: string): string =>
  runtimePath(scene(`/frame-image?${q({ artifact, key })}`), m);

export const shotUrl = (m: MachineId, artifact: string, pin: string): string =>
  runtimePath(scene(`/shot?${q({ artifact, id: pin })}`), m);

/** Builds the model again after a failure. */
export const rebuildScene3d = (m: MachineId, artifact: string, builder = ''): Promise<unknown> =>
  runtimeJson<unknown>(m, scene('/build'), postJson({ artifact, builder }));

/** Asks for the draft frame "as it will be" of a view; answers at once with its state (ready from the cache, or queued). */
export const requestFrame = async (
  m: MachineId,
  artifact: string,
  cam: { n: string } | IScene3dCam,
  frame: number,
  retry = false,
  builder = '',
): Promise<IScene3dFrame> =>
  must(
    readFrame(
      await runtimeJson<unknown>(m, scene('/frame'), postJson({ artifact, cam, frame, retry, builder })),
    ),
  );

export const postPin = async (m: MachineId, artifact: string, draft: IPinDraft): Promise<IScene3dPin> =>
  must(
    readPin(
      await runtimeJson<unknown>(
        m,
        scene('/comments'),
        postJson({
          artifact,
          text: draft.text,
          ...(draft.object ? { object: draft.object } : {}),
          ...(draft.p ? { p: draft.p } : {}),
          cam: draft.cam,
          frame: draft.frame,
          ...(draft.render ? { render: draft.render } : {}),
          ...(draft.shot ? { shot: draft.shot } : {}),
        }),
        60_000,
      ),
    ),
  );

export const resolvePin = async (
  m: MachineId,
  artifact: string,
  id: string,
  resolved: boolean,
): Promise<void> => {
  await runtimeJson<unknown>(m, scene('/comments'), {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ artifact, id, resolved }),
  });
};

export const fixUrl = (m: MachineId, artifact: string, pin: string, which: 'before' | 'after'): string =>
  runtimePath(scene(`/fix?${q({ artifact, id: pin, which })}`), m);

/** What the page shows and the person picked, in one short poll; answers what the agent asked the viewer to do. */
export const reportView = async (
  m: MachineId,
  artifact: string,
  report: { object: string; pin: string; cam: IScene3dCam; frame: number; playing: boolean; acked: number },
  signal?: AbortSignal,
): Promise<readonly IScene3dViewCmd[]> =>
  readViewCmds(
    await runtimeJson<unknown>(m, scene('/view'), { ...postJson({ artifact, ...report }), signal }),
  );

export const clipUrl = (m: MachineId, artifact: string, key: string): string =>
  runtimePath(scene(`/clip-file?${q({ artifact, key })}`), m);

/** A draft animation of a camera over frames start..end, every step-th (0: the runtime picks); answers at once. */
export const requestClip = async (
  m: MachineId,
  artifact: string,
  cam: { n: string } | IScene3dCam,
  start: number,
  end: number,
  step = 0,
  retry = false,
): Promise<IScene3dClip> =>
  must(
    readClip(
      await runtimeJson<unknown>(m, scene('/clip'), postJson({ artifact, cam, start, end, step, retry })),
    ),
  );

/** The plan of a final render on a rented GPU: its estimate and the cloud script's dry run. Nothing is rented. */
export const planFinal = (
  m: MachineId,
  artifact: string,
  cam: { n: string } | IScene3dCam,
  start: number,
  end: number,
): Promise<IScene3dFinalPlan> =>
  runtimeJson<IScene3dFinalPlan>(
    m,
    scene('/final'),
    postJson({ artifact, action: 'plan', cam, start, end }),
    90_000,
  );

/** Starts the render of a plan the person confirmed: the only way a GPU is rented. */
export const startFinal = (m: MachineId, artifact: string, plan: string): Promise<IScene3dFinal> =>
  runtimeJson<IScene3dFinal>(
    m,
    scene('/final'),
    postJson({ artifact, action: 'start', plan, confirm: true }),
  );

/** The clip as a new scene of a video document (a new document when `video` is empty). */
export const clipToVideo = (
  m: MachineId,
  artifact: string,
  clip: string,
  video = '',
): Promise<IScene3dVideoResult> =>
  runtimeJson<IScene3dVideoResult>(m, scene('/to-video'), postJson({ artifact, clip, video }), 60_000);

export const outputUrl = (m: MachineId, artifact: string, key: string, name: string): string =>
  runtimePath(scene(`/output-file?${q({ artifact, key, name })}`), m);

/** The outputs of a camera at some frames for a generator (frames empty: the first, the middle, the last). */
export const requestOutputs = (
  m: MachineId,
  artifact: string,
  cam: { n: string } | IScene3dCam,
  frames: number[] = [],
  masks: string[] = [],
  retry = false,
): Promise<IScene3dOutputs> =>
  runtimeJson<IScene3dOutputs>(m, scene('/outputs'), postJson({ artifact, cam, frames, masks, retry }));

export interface IGenParams {
  outputs: string;
  model: string;
  prompt: string;
  seconds: number;
  source: 'beauty' | 'clay';
}

/** The generator's price of a generation; nothing is started. */
export const estimateGeneration = (
  m: MachineId,
  artifact: string,
  p: IGenParams,
): Promise<{ usd: number; note: string }> =>
  runtimeJson<{ usd: number; note: string }>(
    m,
    scene('/generate'),
    postJson({ artifact, action: 'estimate', ...p }),
    60_000,
  );

/** Starts a generation: spends money, so only from the button of a price the person saw. */
export const startGeneration = (m: MachineId, artifact: string, p: IGenParams): Promise<IScene3dGeneration> =>
  runtimeJson<IScene3dGeneration>(
    m,
    scene('/generate'),
    postJson({ artifact, action: 'start', confirm: true, ...p }),
  );

/** A blender-lab project of the machine to make a 3D scene of. */
export interface IScene3dProject {
  root: string;
  name: string;
  blends: string[];
  scripts: string[];
}

const readProject = (v: unknown): IScene3dProject | null => {
  if (typeof v !== 'object' || v === null) return null;
  const r = v as Record<string, unknown>;
  const strs = (x: unknown): string[] =>
    Array.isArray(x) ? x.filter((s): s is string => typeof s === 'string') : [];
  return typeof r.root === 'string' && typeof r.name === 'string'
    ? { root: r.root, name: r.name, blends: strs(r.blends), scripts: strs(r.scripts) }
    : null;
};

/** The projects the machine finds around its library, or the one in `root` (an absolute folder the person typed). */
export const discoverScene3d = async (
  m: MachineId,
  root = '',
  signal?: AbortSignal,
): Promise<IScene3dProject[]> => {
  const answer = await runtimeJson<{ projects?: unknown }>(
    m,
    scene(root ? `/projects?${q({ root })}` : '/projects'),
    { signal, cache: 'no-store' },
  );
  return (Array.isArray(answer.projects) ? answer.projects : []).flatMap((p) => readProject(p) ?? []);
};

/** A new 3D scene document over a project of the machine; its id. */
export const createScene3d = async (
  m: MachineId,
  input: { title: string; root: string; blend: string; script: string },
): Promise<{ id: string; title: string }> => {
  const a = await runtimeJson<{ id?: unknown; title?: unknown }>(m, scene('/new'), postJson(input), 60_000);
  if (typeof a.id !== 'string') throw new RuntimeError(t('next.failure.http'));
  return { id: a.id, title: typeof a.title === 'string' ? a.title : input.title };
};
