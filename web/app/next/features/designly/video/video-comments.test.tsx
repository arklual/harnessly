// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MachineId } from '@/next/protocol/v2';
import { scene } from '@/next/features/designly/video/test-scene';
import type { IVideoComment, IVideoCommentTarget, IVideoState } from '@/next/features/designly/video/types';

const addVideoComment = vi.fn();
const resolveVideoComment = vi.fn();
const putVideoComment = vi.fn();
const putVideoGuestResolved = vi.fn();

vi.mock('@/next/features/designly/video/api', () => ({
  addVideoComment: (...args: unknown[]) => addVideoComment(...args),
  resolveVideoComment: (...args: unknown[]) => resolveVideoComment(...args),
}));
vi.mock('@/next/stores/video', () => ({
  putVideoComment: (...args: unknown[]) => putVideoComment(...args),
  putVideoGuestResolved: (...args: unknown[]) => putVideoGuestResolved(...args),
}));
vi.mock('@/next/features/designly/deck/guest-comments', () => ({ useGuestComments: () => null }));
vi.mock('@/next/ui/toast', () => ({ toast: vi.fn() }));
vi.mock('@/next/features/designly/sdk/toast', () => ({ toast: vi.fn() }));

const { default: VideoComments } = await import('@/next/features/designly/video/video-comments');

const machine = 'm' as MachineId;
const comment: IVideoComment = {
  id: 'c1',
  page: 'a',
  x: 0,
  y: 0,
  label: '',
  text: 'слишком поздно',
  by: { name: 'Рома', email: 'r@example.org' },
  at: '2026-10-28T10:00:00Z',
  resolved: false,
  resolvedBy: null,
  resolvedAt: null,
  video: { t0: 1000, t1: null, format: '16:9', block: '', frame: '' },
};
const video = (): IVideoState =>
  ({
    rev: 1,
    scenes: [scene('a', 0, 4000)],
    comments: [comment],
    guestResolved: [],
    durMs: 4000,
  }) as unknown as IVideoState;
const target: IVideoCommentTarget = { page: 'a', t0: 1000, t1: null, format: '16:9', pick: null };

let done: (v: unknown) => void;
let fail: (e: unknown) => void;
const pending = () =>
  new Promise((resolve, reject) => {
    done = resolve;
    fail = reject;
  });

beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);

const mount = (onTarget = vi.fn(), t: IVideoCommentTarget | null = null) =>
  render(
    <VideoComments
      machine={machine}
      id="p"
      video={video()}
      canComment
      target={t}
      onTarget={onTarget}
      onSeek={vi.fn()}
    />,
  );

describe('video comments', () => {
  it('shows the chip of what is being commented', () => {
    mount(vi.fn(), target);
    expect(screen.getByText('Scene 1 · 0:01 · a · 16:9')).toBeTruthy();
  });

  it('adds pessimistically: nothing changes before the answer, the composer stays on a failure', async () => {
    const onTarget = vi.fn();
    addVideoComment.mockImplementation(pending);
    mount(onTarget, target);
    fireEvent.change(screen.getByLabelText('Comment text'), { target: { value: 'ну' } });
    fireEvent.click(screen.getByText('Send'));
    expect(addVideoComment).toHaveBeenCalledTimes(1);
    expect(putVideoComment).not.toHaveBeenCalled();
    expect(onTarget).not.toHaveBeenCalled();
    await act(async () => fail(new Error('no')));
    expect(putVideoComment).not.toHaveBeenCalled();
    expect(onTarget).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Comment text')).toBeTruthy();
  });

  it('puts the comment in the store and closes the composer once the runtime took it', async () => {
    const onTarget = vi.fn();
    addVideoComment.mockImplementation(pending);
    mount(onTarget, target);
    fireEvent.change(screen.getByLabelText('Comment text'), { target: { value: 'ну' } });
    fireEvent.click(screen.getByText('Send'));
    await act(async () => done({ ...comment, id: 'c2' }));
    expect(putVideoComment).toHaveBeenCalledWith('p', expect.objectContaining({ id: 'c2' }));
    expect(onTarget).toHaveBeenCalledWith(null);
  });

  it('resolves pessimistically', async () => {
    resolveVideoComment.mockImplementation(pending);
    mount();
    fireEvent.click(screen.getByLabelText('Resolved'));
    expect(resolveVideoComment).toHaveBeenCalledWith(machine, 'p', 'c1', true);
    expect(putVideoComment).not.toHaveBeenCalled();
    await act(async () => done({ id: 'c1', resolved: true, comment: { ...comment, resolved: true } }));
    expect(putVideoComment).toHaveBeenCalledWith('p', expect.objectContaining({ resolved: true }));
  });
});
