// A video state for the shell's tests.
import type { IVideoScene, IVideoState } from '@/next/features/designly/video/types';

export const scene = (n: number, start: number, dur: number): IVideoScene => ({
  id: `P${n}`,
  path: `s${n}.dc.html`,
  title: `Scene ${n}`,
  start,
  dur,
  auto: true,
  transition: { kind: 'fade', ms: 300 },
  moments: [],
  overlays: [],
  words: 10,
  notes: '',
  version: 'v1',
  blank: false,
  keys: {},
});

export const videoState = (patch: Partial<IVideoState> = {}): IVideoState => ({
  rev: 1,
  settings: { v: 1, formats: ['16:9', '9:16'], fps: 30, seconds: null, voice: 'none', subs: true },
  deck: {
    v: 1,
    minutes: 0,
    slides: null,
    tone: 'talk',
    script: false,
    style: null,
    theme: 'co',
    font: 'inter',
  },
  chrome: true,
  ffmpeg: true,
  durMs: 9000,
  scenes: [scene(1, 0, 4000), scene(2, 4000, 5000)],
  comments: [],
  guestResolved: [],
  renders: [
    {
      format: '16:9',
      published: true,
      key: 'k1',
      currentKey: 'k1',
      fresh: true,
      durMs: 9000,
      at: '2026-10-28T10:00:00Z',
    },
    { format: '9:16', published: false, key: null, currentKey: 'k2', fresh: false, durMs: null, at: null },
  ],
  job: null,
  ...patch,
});
