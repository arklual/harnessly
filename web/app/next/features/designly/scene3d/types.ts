// The 3D scene's shapes as the runtime answers them (internal/design/scene3d_types.go). Coordinates of a pin and of a
// camera are Blender's (z up); the viewer turns them into three.js's (y up) in scene-model.ts.

export type TVec3 = readonly [number, number, number];

export interface IScene3dObject {
  n: string;
  t: string;
  p?: string;
  tris?: number;
  hidden?: boolean;
}

export interface IScene3dCamera {
  n: string;
  lens: number;
  /** Vertical field of view in degrees at the render's aspect. */
  fov: number;
  near: number;
  far: number;
  /** The camera's path: [frame, x, y, z, tx, ty, tz, fov] samples (Blender's axes; t* a point 5 m ahead). */
  track: readonly (readonly number[])[];
}

export interface IScene3dMarker {
  f: number;
  n: string;
  cam?: string;
}

export interface IScene3dStats {
  tris: number;
  trisOut: number;
  objects: number;
  animated: number;
  missingImages?: string[];
  unappliedScale?: string[];
  offCamera?: string[];
}

export interface IScene3dMeta {
  version: number;
  fps: number;
  start: number;
  end: number;
  res: readonly [number, number];
  camera?: string;
  objects: IScene3dObject[];
  cameras: IScene3dCamera[];
  markers: IScene3dMarker[];
  /** min xyz, max xyz (Blender's axes). */
  bounds: readonly [number, number, number, number, number, number];
  stats: IScene3dStats;
  /** Objects the glTF's actions do not carry (drivers, constraints): their local transform at sampled frames, "e": location,
   *  XYZ euler, scale; "q": location, quaternion (w x y z), scale. */
  bake?: Record<string, { m: 'e' | 'q'; rows: number[][] }>;
}

export const MODEL_STATUS = { none: 'none', building: 'building', ready: 'ready', failed: 'failed' } as const;
export type TModelStatus = (typeof MODEL_STATUS)[keyof typeof MODEL_STATUS];

export interface IScene3dModel {
  status: TModelStatus;
  hash?: string;
  bytes?: number;
  error?: string;
  meta?: IScene3dMeta;
}

/** A view of the scene: a camera of the scene by name, or a free one. */
export interface IScene3dCam {
  n?: string;
  pos: TVec3;
  target: TVec3;
  fov: number;
}

export interface IScene3dAnchor {
  object?: string;
  p?: TVec3;
  cam?: IScene3dCam;
  frame: number;
  shot?: boolean;
  render?: string;
  /** How an agent closed the pin: the frames of its view before and after the change. */
  fix?: IScene3dFix;
}

export interface IScene3dFix {
  before?: string;
  after?: string;
  note?: string;
  at: string;
}

/** What the agent asks the viewer to do. */
export interface IScene3dViewCmd {
  seq: number;
  cam?: IScene3dCam;
  frame?: number;
  object?: string;
  pin?: string;
}

export interface IScene3dPin {
  id: string;
  label: string;
  text: string;
  by: { name: string; email: string };
  at: string;
  resolved: boolean;
  scene: IScene3dAnchor;
  /** The cached draft frame's path under the project, "" when none. */
  frame: string;
}

export const FRAME_STATUS = {
  queued: 'queued',
  building: 'building',
  ready: 'ready',
  failed: 'failed',
} as const;
export type TFrameStatus = (typeof FRAME_STATUS)[keyof typeof FRAME_STATUS];

export interface IScene3dFrame {
  key: string;
  status: TFrameStatus;
  cam?: IScene3dCam;
  frame: number;
  engine?: string;
  error?: string;
  cached?: boolean;
  /** The builder that drew it; absent: this machine. */
  where?: string;
}

export interface IScene3dOrigin {
  project: string;
  title: string;
  hash: string;
  cam: IScene3dCam;
  start: number;
  end: number;
  step: number;
  kind: 'draft' | 'final' | 'generated';
  engine?: string;
  where?: string;
  width: number;
  height: number;
  createdAt: string;
}

export interface IScene3dClip {
  key: string;
  status: 'queued' | 'building' | 'ready' | 'failed';
  done: number;
  total: number;
  frames?: number;
  fps?: number;
  seconds?: number;
  error?: string;
  origin: IScene3dOrigin;
}

export interface IScene3dFinalPlan {
  id: string;
  spec: { cam: IScene3dCam; start: number; end: number; step: number };
  frames: number;
  seconds: number;
  hours: number;
  estimateRub: number;
  dry: string;
  warnings: string[];
  expires: string;
  hash: string;
}

export interface IScene3dFinal {
  id: string;
  status: 'building' | 'ready' | 'failed';
  phase: string;
  error?: string;
  clip?: string;
  cloud?: string;
  started: string;
  Plan: IScene3dFinalPlan;
}

export interface IScene3dVideoResult {
  video: string;
  scene: string;
  page: string;
  folder: string;
  new: boolean;
}

export interface IScene3dKeyframe {
  frame: number;
  seconds: number;
  beauty?: string;
  clay?: string;
  depth?: string;
  normals?: string;
}

export interface IScene3dOutputs {
  key: string;
  status: 'queued' | 'building' | 'ready' | 'failed';
  error?: string;
  manifest?: {
    camera: IScene3dCam;
    keyframes: IScene3dKeyframe[];
    masks: { object: string; frame: number; file: string }[];
    passes: string[];
    where?: string;
  };
}

export interface IScene3dGeneration {
  id: string;
  outputs: string;
  model: string;
  prompt: string;
  state: string;
  estimateUsd: number;
  error?: string;
  clip?: string;
  startedAt: string;
}

export interface IScene3dState {
  rev: number;
  title: string;
  settings: { root: string; blend: string; script?: string };
  blender: boolean;
  engine: string;
  model: IScene3dModel;
  pins: IScene3dPin[];
  guestResolved: string[];
  frames: IScene3dFrame[];
  clips: IScene3dClip[];
  finals: IScene3dFinal[];
  outputs: IScene3dOutputs[];
  generations: IScene3dGeneration[];
  generator: boolean;
}

/** What the viewer shows the object as. */
export const VIEW_MODE = { material: 'material', clay: 'clay', wire: 'wire' } as const;
export type TViewMode = (typeof VIEW_MODE)[keyof typeof VIEW_MODE];

/** A pin about to be saved: the viewer's place, the person's words. */
export interface IPinDraft {
  object?: string;
  p?: TVec3;
  cam: IScene3dCam;
  frame: number;
  text: string;
  render?: string;
  shot?: string;
}
