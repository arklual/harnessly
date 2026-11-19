import { describe, expect, it } from 'vitest';
import {
  blenderToThree,
  canvasFov,
  poseAt,
  cameraAt,
  checksOf,
  clock,
  frameOf,
  pathTo,
  threeToBlender,
  timeOf,
  treeOf,
} from '@/next/features/designly/scene3d/scene-model';
import type { IScene3dMeta } from '@/next/features/designly/scene3d/types';

const meta: IScene3dMeta = {
  version: 1,
  fps: 24,
  start: 1,
  end: 241,
  res: [1920, 1080],
  camera: 'cam1',
  objects: [
    { n: 'car', t: 'MESH' },
    { n: 'wheel', t: 'MESH', p: 'car' },
    { n: 'bolt', t: 'MESH', p: 'wheel' },
    { n: 'stray', t: 'MESH', p: 'gone' },
  ],
  cameras: [{ n: 'cam1', lens: 35, fov: 40, near: 0.1, far: 100, track: [] }],
  markers: [
    { f: 100, n: 'b', cam: 'cam3' },
    { f: 1, n: 'a', cam: 'cam1' },
    { f: 50, n: 'note' },
  ],
  bounds: [-1, -1, 0, 1, 1, 2],
  stats: { tris: 100, trisOut: 100, objects: 4, animated: 0 },
};

describe('axes', () => {
  it('goes there and back', () => {
    const v = [1.5, -2, 0.25] as const;
    expect(blenderToThree(v)).toEqual([1.5, 0.25, 2]);
    expect(threeToBlender(blenderToThree(v))).toEqual([1.5, -2, 0.25]);
  });
});

describe('timeline', () => {
  it('takes the camera of the last marker that binds one', () => {
    expect(cameraAt(meta, 1)).toBe('cam1');
    expect(cameraAt(meta, 99)).toBe('cam1');
    expect(cameraAt(meta, 100)).toBe('cam3');
    expect(cameraAt(meta, 240)).toBe('cam3');
    expect(cameraAt({ ...meta, markers: [] }, 5)).toBe('cam1');
  });
  it('turns frames into seconds and back inside the scene', () => {
    expect(timeOf(meta, 25)).toBe(1);
    expect(frameOf(meta, 1)).toBe(25);
    expect(frameOf(meta, -4)).toBe(1);
    expect(frameOf(meta, 999)).toBe(241);
    expect(clock(7.5)).toBe('0:07.5');
    expect(clock(75.25)).toBe('1:15.3');
    expect(clock(9.96)).toBe('0:10.0');
    expect(clock(59.97)).toBe('1:00.0');
    expect(clock(-3)).toBe('0:00.0');
  });
});

describe('tree', () => {
  it('nests by parent and keeps an orphan as a root', () => {
    const roots = treeOf(meta.objects);
    expect(roots.map((r) => r.object.n)).toEqual(['car', 'stray']);
    expect(roots[0]?.children[0]?.children[0]?.object.n).toBe('bolt');
    expect(pathTo(meta.objects, 'bolt')).toEqual(['car', 'wheel', 'bolt']);
    expect(pathTo(meta.objects, 'nope')).toEqual([]);
  });
  it('survives a parent loop', () => {
    const loop = [
      { n: 'a', t: 'EMPTY', p: 'b' },
      { n: 'b', t: 'EMPTY', p: 'a' },
    ];
    expect(treeOf(loop)).toEqual([]);
    expect(pathTo(loop, 'a').length).toBeLessThanOrEqual(64);
  });
});

describe('checks', () => {
  it('reports only what is wrong', () => {
    expect(checksOf(meta)).toEqual([]);
    const bad = checksOf({
      ...meta,
      stats: { ...meta.stats, tris: 5_000_000, missingImages: ['a.png'], offCamera: ['x', 'y'] },
    });
    expect(bad.map((c) => c.code)).toEqual(['tris', 'textures', 'offCamera']);
    expect(bad[2]?.count).toBe(2);
  });
});

describe('camera path', () => {
  const cam = {
    n: 'c',
    lens: 35,
    fov: 40,
    near: 0.1,
    far: 100,
    track: [
      [1, 0, 0, 1, 0, 5, 1, 40],
      [11, 10, 0, 1, 10, 5, 1, 20],
      [12, 50, 0, 1, 50, 5, 1, 20],
    ],
  };
  it('holds before and after, mixes between', () => {
    expect(poseAt(cam, -5).pos).toEqual([0, 0, 1]);
    expect(poseAt(cam, 6).pos).toEqual([5, 0, 1]);
    expect(poseAt(cam, 6).fov).toBe(30);
    expect(poseAt(cam, 99).pos).toEqual([50, 0, 1]);
  });
  it('a camera without a path stays where it can be seen', () => {
    expect(poseAt({ ...cam, track: [] }, 3).fov).toBe(40);
  });
  it('widens the canvas camera when the guide is limited by width', () => {
    expect(canvasFov(40, 16 / 9, 2)).toBe(40);
    expect(canvasFov(40, 16 / 9, 16 / 9)).toBe(40);
    expect(canvasFov(40, 16 / 9, 1)).toBeGreaterThan(40);
  });
});
