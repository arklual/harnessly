// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MachineId } from '@/next/protocol/v2';
import type { IVideoState } from '@/next/features/designly/video/types';
import { videoState } from '@/next/features/designly/video/video-fixture';

let video: IVideoState | undefined;
let phone = false;
const goTo = vi.fn();
const startVideoRender = vi.fn();

vi.mock('@/next/stores/video', () => ({
  useVideo: () => video,
  changeVideo: vi.fn(),
  putVideoJob: vi.fn(),
}));
vi.mock('@/next/stores/deck', () => ({ setDeck: vi.fn() }));
vi.mock('@/next/routes/routes', () => ({
  goTo: (...a: unknown[]) => goTo(...a),
  ROUTE: { designly: 'designly' },
}));
vi.mock('@/next/ui/use-media', () => ({ useMedia: () => phone, usePhone: () => phone }));
vi.mock('@/next/features/designly/deck/api', () => ({
  fetchDeck: vi.fn(() => new Promise(() => undefined)),
  putDeckSettings: vi.fn(),
}));
vi.mock('@/next/features/designly/video/api', () => ({
  startVideoRender: (...a: unknown[]) => startVideoRender(...a),
  videoRenderJob: vi.fn(),
  reportVideoView: vi.fn(() => new Promise(() => undefined)),
  videoFileHref: (_m: string, _id: string, f: string) => `file/${f}`,
}));
vi.mock('@/next/features/designly/deck/deck-share', () => ({ default: () => <div data-testid="share" /> }));
vi.mock('@/next/features/designly/video/video-player', () => ({
  default: (p: { format: string; source: string }) => (
    <div data-testid="player" data-format={p.format} data-source={p.source} />
  ),
}));
vi.mock('@/next/features/designly/video/video-timeline', () => ({
  default: () => <div data-testid="timeline" />,
}));
vi.mock('@/next/features/designly/video/video-comments', () => ({
  default: () => <div data-testid="comments" />,
}));

// jsdom on a loaded machine is slow; the tests are not.
vi.setConfig({ testTimeout: 30000 });

const { default: VideoView } = await import('@/next/features/designly/video/video-view');

const props = (over: Record<string, unknown> = {}) => ({
  machine: 'm1' as MachineId,
  id: 'vid',
  title: 'Promo',
  boards: [],
  path: 's1.dc.html',
  onGo: vi.fn(),
  src: 'frame/s1',
  frameSrc: (p: string) => `frame/${p}`,
  slotError: null,
  onReload: vi.fn(),
  maxed: false,
  onToggleMax: vi.fn(),
  onOpenDrawer: vi.fn(),
  fallback: <div data-testid="fallback" />,
  ...over,
});

beforeEach(() => {
  video = undefined;
  phone = false;
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('VideoView', () => {
  it('says the video is on its way while it is read, never an empty screen', () => {
    const { container } = render(<VideoView {...props()} />);
    expect(container.textContent).toContain('Opening the video');
  });

  it('says why when the video could not be read at all', () => {
    const { container } = render(<VideoView {...props({ videoError: 'boom' })} />);
    expect(container.textContent).toContain('boom');
    expect(screen.queryByTestId('player')).toBeNull();
  });

  it('shows a different state for a video without scenes', () => {
    video = videoState({ scenes: [], durMs: 0 });
    render(<VideoView {...props()} />);
    expect(screen.queryByTestId('player')).toBeNull();
    expect(screen.queryByTestId('timeline')).toBeNull();
    expect(document.querySelector('.hx-video__empty')).not.toBeNull();
  });

  it('shows the player and the timeline for a video with scenes', () => {
    video = videoState();
    render(<VideoView {...props()} />);
    expect(screen.getByTestId('player').getAttribute('data-format')).toBe('16:9');
    expect(screen.getByTestId('timeline')).not.toBeNull();
  });

  it('switches the format, and plays the MP4 only where one is published', () => {
    video = videoState();
    render(<VideoView {...props()} />);
    const group = document.querySelector('[data-k="format"]') as HTMLElement;
    fireEvent.click(group.querySelectorAll('button')[1]);
    expect(screen.getByTestId('player').getAttribute('data-format')).toBe('9:16');
    const source = document.querySelector('[data-k="source"]') as HTMLElement;
    const mp4 = source.querySelectorAll('button')[1];
    expect((mp4 as HTMLButtonElement).disabled).toBe(true);
  });

  it('gives a phone the list of scenes instead of the timeline', () => {
    phone = true;
    video = videoState();
    render(<VideoView {...props()} />);
    expect(screen.queryByTestId('timeline')).toBeNull();
    expect(document.querySelectorAll('.hx-video-scenes li').length).toBe(2);
  });

  it('marks the render button when the MP4 is outdated', () => {
    const v = videoState();
    video = { ...v, renders: [{ ...v.renders[0], currentKey: 'new' }, v.renders[1]] };
    render(<VideoView {...props()} />);
    expect(document.querySelector('[data-k="render"]')?.getAttribute('data-mark')).toBe('1');
  });

  it('starts a render only after the popover is open, pessimistically', async () => {
    video = videoState();
    startVideoRender.mockResolvedValue({ id: 'j' });
    render(<VideoView {...props()} />);
    fireEvent.click(document.querySelector('[data-k="render"]') as HTMLElement);
    const buttons = [...document.querySelectorAll<HTMLButtonElement>('.hx-video-render__acts button')];
    await act(async () => {
      fireEvent.click(buttons[0]);
    });
    expect(startVideoRender).toHaveBeenCalledWith('m1', 'vid', '16:9', false);
  });
});
