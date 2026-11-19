import { describe, expect, it } from 'vitest';
import {
  readClip,
  readFrame,
  readPin,
  readScene3dState,
  readViewCmds,
} from '@/next/features/designly/scene3d/wire';

const model = {
  status: 'ready',
  hash: 'abc',
  bytes: 10,
  meta: {
    version: 1,
    fps: 24,
    start: 1,
    end: 240,
    res: [1920, 1080],
    camera: 'cam1',
    objects: [{ n: 'car', t: 'MESH', tris: 3 }, { t: 'MESH' }],
    cameras: [
      {
        n: 'cam1',
        lens: 35,
        fov: 40,
        near: 0.1,
        far: 100,
        track: [
          [1, 0, 0, 1, 0, 5, 1, 40],
          [2, 0],
        ],
      },
    ],
    markers: [{ f: 1, n: 'a', cam: 'cam1' }],
    bounds: [-1, -1, 0, 1, 1, 2],
    stats: { tris: 3, trisOut: 3, objects: 1, animated: 0 },
  },
};

describe('the scene state as read', () => {
  it('takes what is in the shape and drops what is not', () => {
    const s = readScene3dState({
      rev: 5,
      title: 'T',
      settings: { root: '/r', blend: 'b.blend' },
      blender: true,
      engine: 'cycles',
      model,
      comments: [],
      frames: [],
      clips: [],
      finals: [],
      outputs: [],
      generations: [],
      generator: true,
    });
    expect(s?.rev).toBe(5);
    expect(s?.generator).toBe(true);
    expect(s?.model.meta?.objects.map((o) => o.n)).toEqual(['car']);
    expect(s?.model.meta?.cameras[0]?.track).toEqual([[1, 0, 0, 1, 0, 5, 1, 40]]);
  });
  it('is null for what is not a scene', () => {
    expect(readScene3dState(null)).toBeNull();
    expect(readScene3dState({ model: { status: 'weird' }, settings: {} })).toBeNull();
  });
});

describe('pins', () => {
  const pin = {
    id: 'P1',
    view: 'scene3d',
    text: 'darker',
    label: 'body',
    by: { name: 'Anya', email: 'a@example.com' },
    at: '2026-11-18T10:00:00Z',
    resolved: true,
    frame: '',
    scene: {
      object: 'body',
      p: [1, 2, 3],
      cam: { n: 'cam1', pos: [1, 1, 1], target: [0, 0, 0], fov: 40 },
      frame: 7,
      shot: true,
      fix: { before: 'b', after: 'a', note: 'done', at: 'x' },
    },
  };
  it('keeps the anchor and how it was fixed', () => {
    const p = readPin(pin);
    expect(p?.scene.object).toBe('body');
    expect(p?.scene.p).toEqual([1, 2, 3]);
    expect(p?.scene.cam?.n).toBe('cam1');
    expect(p?.scene.fix).toEqual({ before: 'b', after: 'a', note: 'done', at: 'x' });
  });
  it('ignores a comment that is not a pin', () => {
    expect(readPin({ ...pin, view: 'video' })).toBeNull();
    expect(readPin({ ...pin, scene: null })).toBeNull();
  });
});

describe('frames, clips and commands', () => {
  it('reads a frame by its status', () => {
    expect(readFrame({ key: 'k', status: 'ready', frame: 3, where: 'farm' })?.where).toBe('farm');
    expect(readFrame({ key: 'k', status: 'sideways' })).toBeNull();
  });
  it('reads a clip with its origin', () => {
    const c = readClip({
      key: 'k',
      status: 'ready',
      done: 5,
      total: 5,
      frames: 5,
      fps: 2,
      seconds: 2.5,
      origin: {
        project: 'p',
        title: 'T',
        hash: 'h',
        cam: { n: 'c' },
        start: 1,
        end: 49,
        step: 12,
        kind: 'generated',
        width: 640,
        height: 360,
        createdAt: 'x',
      },
    });
    expect(c?.origin.kind).toBe('generated');
    expect(c?.origin.step).toBe(12);
    expect(readClip({ key: 'k' })).toBeNull();
  });
  it('reads the agent’s commands in order of the runtime', () => {
    expect(
      readViewCmds({ cmds: [{ seq: 2, pin: 'P1' }, { nope: 1 }, { seq: 3, frame: 9, cam: { n: 'cam2' } }] }),
    ).toEqual([
      { seq: 2, pin: 'P1' },
      { seq: 3, frame: 9, cam: { n: 'cam2', pos: [0, 0, 0], target: [0, 0, 0], fov: 0 } },
    ]);
    expect(readViewCmds(null)).toEqual([]);
  });
});
