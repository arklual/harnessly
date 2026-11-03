// @vitest-environment jsdom
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import VideoPlayer, { type IVideoPlayerProps } from '@/next/features/designly/video/video-player';
import { noOverlays } from '@/next/features/designly/video/test-scene';
import type { IVideoScene, IVideoState } from '@/next/features/designly/video/types';

const ORIGIN = 'https://slot.test';

// the shared machine is slow to render
vi.setConfig({ testTimeout: 30_000 });

const scene = (n: number, start: number, dur: number): IVideoScene => ({
  id: `p${n}`,
  path: `s${n}.dc.html`,
  title: `Scene ${n}`,
  start,
  dur,
  auto: false,
  transition: { kind: 'fade', ms: 400 },
  moments: [],
  overlays: [],
  words: 0,
  notes: 'One. Two.',
  version: `v${n}`,
  blank: false,
  keys: {},
});

const state = (): IVideoState => ({
  rev: 1,
  settings: { v: 1, formats: ['16:9', '9:16'], fps: 30, seconds: null, voice: 'none', subs: true },
  deck: {} as IVideoState['deck'],
  chrome: true,
  ffmpeg: true,
  durMs: 12_000,
  scenes: [scene(1, 0, 4000), scene(2, 4000, 6000), scene(3, 10_000, 2000)],
  comments: [],
  guestResolved: [],
  renders: [],
  job: null,
});

const props = (over: Partial<IVideoPlayerProps> = {}): IVideoPlayerProps => ({
  overlays: noOverlays,
  machine: 'm1' as IVideoPlayerProps['machine'],
  id: 'proj',
  video: state(),
  format: '16:9',
  src: `${ORIGIN}/first/s1.dc.html`,
  path: 's1.dc.html',
  frameSrc: (path) => `${ORIGIN}/open/${path}`,
  source: 'live',
  onSource: vi.fn(),
  time: 0,
  onTime: vi.fn(),
  range: null,
  onRange: vi.fn(),
  loop: false,
  onComment: vi.fn(),
  onPick: vi.fn(),
  ...over,
});

const frames = (container: HTMLElement) => [...container.querySelectorAll('iframe')];

const say = (source: Window | null, data: unknown, origin = ORIGIN) =>
  act(() => {
    window.dispatchEvent(new MessageEvent('message', { data, source, origin }));
  });

beforeEach(() => {
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('VideoPlayer', () => {
  it('holds the scene on screen and the next one in two frames', () => {
    const { container } = render(<VideoPlayer {...props()} />);
    const [a] = frames(container);
    say(a.contentWindow, { type: 'tk:video-ready', moments: [] });
    const [first, b] = frames(container);
    expect(first.src).toBe(`${ORIGIN}/first/s1.dc.html`);
    expect(b.src).toBe(`${ORIGIN}/open/s2.dc.html`);
  });

  it('loads the next scene only after the frame with the ticket has set the cookie', () => {
    const { container } = render(<VideoPlayer {...props()} />);
    expect(frames(container)).toHaveLength(1);
    say(frames(container)[0].contentWindow, { type: 'tk:video-ready', moments: [] });
    expect(frames(container)).toHaveLength(2);
  });

  it('hears a scene whose ticket came after the player mounted', () => {
    const { container, rerender } = render(<VideoPlayer {...props({ src: null })} />);
    expect(frames(container)).toHaveLength(0);
    rerender(<VideoPlayer {...props()} />);
    const [a] = frames(container);
    const toA = vi.spyOn(a.contentWindow as Window, 'postMessage');
    say(a.contentWindow, { type: 'tk:video-ready', moments: [] });
    expect(toA).toHaveBeenCalledWith({ type: 'tk:video-seek', t: 0 }, ORIGIN);
  });

  it('swaps the frames at a scene boundary: the next scene was already in the other frame', () => {
    const { container, rerender } = render(<VideoPlayer {...props()} />);
    say(frames(container)[0].contentWindow, { type: 'tk:video-ready', moments: [] });
    const [a, b] = frames(container);
    rerender(<VideoPlayer {...props({ time: 4000 })} />);
    expect(frames(container)[0]).toBe(a);
    expect(frames(container)[1]).toBe(b);
    // scene 2 stays in the same frame (b); the frame that was on screen takes the scene after it
    expect(b.src).toBe(`${ORIGIN}/open/s2.dc.html`);
    expect(a.src).toBe(`${ORIGIN}/open/s3.dc.html`);
  });

  it('seeks the frame of the scene the playhead is in once it says it is ready', () => {
    const { container, rerender } = render(<VideoPlayer {...props()} />);
    rerender(<VideoPlayer {...props({ time: 7000 })} />);
    const [a, b] = frames(container);
    const toA = vi.spyOn(a.contentWindow as Window, 'postMessage');
    const toB = vi.spyOn(b.contentWindow as Window, 'postMessage');
    say(b.contentWindow, { type: 'tk:video-ready', moments: [] });
    expect(toB).toHaveBeenCalledWith({ type: 'tk:video-seek', t: 3000 }, ORIGIN);
    expect(toA).not.toHaveBeenCalled();
    // the frame with the next scene waits at its start
    say(a.contentWindow, { type: 'tk:video-ready', moments: [] });
    expect(toA).toHaveBeenCalledWith({ type: 'tk:video-pause' }, ORIGIN);
    expect(toA).toHaveBeenCalledWith({ type: 'tk:video-seek', t: 0 }, ORIGIN);
  });

  it('opens the ticket address once: back on its scene after it left, the frame takes the plain address', () => {
    const { container, rerender } = render(<VideoPlayer {...props()} />);
    const [a] = frames(container);
    say(a.contentWindow, { type: 'tk:video-ready', moments: [] });
    rerender(<VideoPlayer {...props({ time: 11_000 })} />);
    // scene 3 is in the first frame, no frame holds scene 1: the ticket is spent
    expect(a.src).toBe(`${ORIGIN}/open/s3.dc.html`);
    rerender(<VideoPlayer {...props({ time: 500 })} />);
    expect(frames(container)[0].src).toBe(`${ORIGIN}/open/s1.dc.html`);
  });

  it('gives the ticket address to the scene of the board it was made for', () => {
    const { container } = render(
      <VideoPlayer {...props({ path: 's2.dc.html', src: `${ORIGIN}/first/s2.dc.html` })} />,
    );
    const [ticket] = frames(container);
    expect(ticket.src).toBe(`${ORIGIN}/first/s2.dc.html`);
    say(ticket.contentWindow, { type: 'tk:video-ready', moments: [] });
    const [a, b] = frames(container);
    expect(a.src).toBe(`${ORIGIN}/open/s1.dc.html`);
    expect(b.src).toBe(`${ORIGIN}/first/s2.dc.html`);
  });

  it('shows the frame of the current scene only when it is ready', () => {
    const { container } = render(<VideoPlayer {...props()} />);
    const [a] = frames(container);
    expect(a.dataset.on).toBe('1');
    say(a.contentWindow, { type: 'tk:video-ready', moments: [] });
    const b = frames(container)[1];
    expect(b.dataset.on).toBe('0');
    expect(a.dataset.on).toBe('1');
  });

  it('ignores messages from other windows and from other origins', () => {
    const { container, rerender } = render(<VideoPlayer {...props()} />);
    rerender(<VideoPlayer {...props({ time: 5000 })} />);
    const [a, b] = frames(container);
    const toA = vi.spyOn(a.contentWindow as Window, 'postMessage');
    const toB = vi.spyOn(b.contentWindow as Window, 'postMessage');
    say(window, { type: 'tk:video-ready', moments: [] });
    say(null, { type: 'tk:video-ready', moments: [] });
    say(b.contentWindow, { type: 'tk:video-ready', moments: [] }, 'https://evil.test');
    expect(toA).not.toHaveBeenCalled();
    expect(toB).not.toHaveBeenCalled();
  });

  it('shows the subtitle line of the scene in the live player', () => {
    const { container } = render(<VideoPlayer {...props({ time: 3500 })} />);
    expect(container.querySelector('.hx-video__sub')?.textContent).toBe('Two.');
  });

  it('has no format or source switch of its own: the bar holds them', () => {
    const { queryAllByRole } = render(<VideoPlayer {...props()} />);
    expect(queryAllByRole('radio')).toEqual([]);
  });

  it('falls back to live when the MP4 is not there', () => {
    const onSource = vi.fn();
    render(<VideoPlayer {...props({ source: 'mp4', onSource })} />);
    expect(onSource).toHaveBeenCalledWith('live');
  });
});
