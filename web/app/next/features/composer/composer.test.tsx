// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Composer, { type IComposerProps } from '@/next/features/composer/composer';
import Queue from '@/next/features/composer/queue';
import type { QueueRow } from '@/next/features/composer/types';

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const props = (over: Partial<IComposerProps> = {}): IComposerProps => ({
  value: 'also check the logs',
  onChange: vi.fn(),
  onSubmit: vi.fn(),
  mode: 'working',
  onStop: () => Promise.resolve(),
  picker: null,
  attachments: [],
  onFiles: vi.fn(),
  onRemoveAttachment: vi.fn(),
  onRetryAttachment: vi.fn(),
  recording: null,
  onCancelRecording: vi.fn(),
  onFinishRecording: vi.fn(),
  maxText: 1000,
  ...over,
});

describe('Composer while the agent works', () => {
  it('shows one button: the arrow queues a written message, no stop or «Now» beside it', () => {
    const onSubmit = vi.fn();
    render(<Composer {...props({ onSubmit })} />);
    expect(screen.queryByRole('button', { name: 'Now' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Stop the agent' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Queue it · ⌃↵ straight into the turn' }));
    expect(onSubmit).toHaveBeenLastCalledWith('queue');
  });

  it('shows the stop instead of the arrow while the field is empty', () => {
    const { rerender } = render(<Composer {...props({ value: '' })} />);
    expect(screen.getByRole('button', { name: 'Stop the agent' })).toBeTruthy();
    rerender(<Composer {...props({ mode: 'idle' })} />);
    expect(screen.queryByRole('button', { name: 'Stop the agent' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Send' })).toBeTruthy();
  });

  it('Ctrl+Enter sends into the turn, Enter into the queue', () => {
    const onSubmit = vi.fn();
    render(<Composer {...props({ onSubmit })} />);
    const field = screen.getByRole('textbox');
    fireEvent.keyDown(field, { key: 'Enter', ctrlKey: true });
    expect(onSubmit).toHaveBeenLastCalledWith('now');
    fireEvent.keyDown(field, { key: 'Enter' });
    expect(onSubmit).toHaveBeenLastCalledWith('queue');
  });
});

describe('Queue rows', () => {
  const row: QueueRow = {
    clientId: 'c1',
    text: 'later',
    attachments: [],
    state: { k: 'queued', position: 1, reason: 'agent-busy' },
  };
  const queue = (onEdit = vi.fn(() => Promise.resolve())) =>
    render(
      <Queue
        rows={[row]}
        paused={null}
        waitingLimit={false}
        onEdit={onEdit}
        onRemove={vi.fn()}
        onResume={vi.fn()}
        onRetry={vi.fn()}
      />,
    );

  it('a waiting row has the pencil and the bin and no note and no «Now»', () => {
    queue();
    expect(screen.queryByText('goes when the agent finishes')).toBeNull();
    expect(screen.queryByRole('button', { name: /Now/ })).toBeNull();
    expect(screen.getByRole('button', { name: 'Edit' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Remove from the queue' })).toBeTruthy();
  });

  it('the pencil asks to take the message back into the field', () => {
    const onEdit = vi.fn(() => Promise.resolve());
    queue(onEdit);
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(onEdit).toHaveBeenCalledWith('c1');
  });

  it('a message typed into the turn is on its way, not waiting: no pencil, and it says the agent reads it after the step', () => {
    render(
      <Queue
        rows={[{ ...row, steer: true, handed: true, state: { k: 'in-input' } }]}
        paused={null}
        waitingLimit={false}
        onEdit={vi.fn()}
        onRemove={vi.fn()}
        onResume={vi.fn()}
        onRetry={vi.fn()}
      />,
    );
    expect(screen.getByText("In the agent's queue — read after the current step")).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
  });

  it('a message held by text in the terminal\u2019s input line says so, steered or not', () => {
    render(
      <Queue
        rows={[
          { ...row, state: { k: 'queued', position: 1, reason: 'blocked-text' } },
          {
            ...row,
            clientId: 'c2',
            steer: true,
            state: { k: 'queued', position: 2, reason: 'blocked-text' },
          },
        ]}
        paused={null}
        waitingLimit={false}
        onRemove={vi.fn()}
        onResume={vi.fn()}
        onRetry={vi.fn()}
      />,
    );
    expect(screen.getAllByText("the terminal's input line holds text")).toHaveLength(2);
  });
});

describe('A paused queue row', () => {
  const row: QueueRow = {
    clientId: 'c1',
    text: 'start',
    attachments: [],
    state: { k: 'queued', position: 1, reason: 'paused' },
  };
  const paused = (reason: 'launch-failed' | 'user') =>
    render(
      <Queue
        rows={[row]}
        paused={{ reason, at: 1 }}
        waitingLimit={false}
        onRemove={vi.fn()}
        onResume={vi.fn()}
        onRetry={vi.fn()}
      />,
    );

  it('says the agent did not start when a failed launch holds it', () => {
    paused('launch-failed');
    expect(screen.getByText('the agent did not start')).toBeTruthy();
  });

  it('is only paused when the person paused it', () => {
    paused('user');
    expect(screen.getByText('paused')).toBeTruthy();
  });
});

describe("Queue row with a design's notes", () => {
  it('shows the words and a line per note, never the <annotation> markup', () => {
    const page = `/a/d/p/${encodeURIComponent('Главная')}.html`;
    const text = [
      'Поправь шапку',
      `<annotation n="1" url="${page}" target="port:1">\nelement: main > span — «Шапка»\nhtml: &lt;span&gt;Шапка&lt;/span&gt;\ncomment: крупнее\n</annotation>`,
    ].join('\n\n');
    const { container } = render(
      <Queue
        rows={[
          {
            clientId: 'c1',
            text,
            attachments: [],
            state: { k: 'queued', position: 1, reason: 'agent-busy' },
          },
        ]}
        paused={null}
        waitingLimit={false}
        onEdit={vi.fn()}
        onRemove={vi.fn()}
        onResume={vi.fn()}
        onRetry={vi.fn()}
      />,
    );
    const shown = container.querySelector('.hx-queue__text')?.textContent ?? '';
    expect(shown).toContain('Поправь шапку');
    expect(shown).toContain('Главная — крупнее');
    expect(shown).not.toContain('<annotation');
    expect(shown).not.toContain('span');
  });
});

describe('Queue row controls', () => {
  const waiting = (clientId: string, over: Partial<QueueRow> = {}): QueueRow => ({
    clientId,
    text: `message ${clientId}`,
    attachments: [],
    state: { k: 'queued', position: 1, reason: 'behind' },
    ...over,
  });
  const thumb = { id: 'up_1', name: 'shot.png', kind: 'image', mime: 'image/png' } as const;
  const rowsOf =
    (...rows: QueueRow[]) =>
    (
      onMove?: (id: string, before: string | null) => Promise<unknown>,
      onSendNow = vi.fn(() => Promise.resolve()),
    ) =>
      render(
        <Queue
          rows={rows}
          paused={null}
          waitingLimit={false}
          onEdit={vi.fn()}
          onSendNow={onSendNow}
          onMove={onMove}
          onRemove={vi.fn()}
          onResume={vi.fn()}
          onRetry={vi.fn()}
        />,
      );

  it('every waiting row, with a picture or without, ends in «Send now», the pencil and the bin, in that order', () => {
    rowsOf(waiting('a'), waiting('b', { attachments: [thumb] }))();
    for (const row of screen.getAllByRole('listitem')) {
      const names = Array.from(row.querySelectorAll('button')).map((b) => b.getAttribute('aria-label'));
      expect(names).toEqual(['Send now', 'Edit', 'Remove from the queue']);
    }
    const withPicture = screen.getAllByRole('listitem')[1];
    const order = Array.from(withPicture.children).map((el) => el.className.split(' ')[0] || el.tagName);
    expect(order.indexOf('hx-queue__files')).toBeLessThan(order.indexOf('hx-queue__del'));
  });

  it('«Send now» sends the row it stands on', () => {
    const onSendNow = vi.fn(() => Promise.resolve());
    rowsOf(waiting('a'), waiting('b'))(undefined, onSendNow);
    fireEvent.click(screen.getAllByRole('button', { name: 'Send now' })[1]);
    expect(onSendNow).toHaveBeenCalledWith('b');
  });

  it('draws a grip on each waiting row when the runtime can reorder, and none without it', () => {
    const view = rowsOf(waiting('a'), waiting('b'));
    view(undefined);
    expect(screen.queryAllByRole('button', { name: 'Drag to change the order' })).toHaveLength(0);
    cleanup();
    view(vi.fn(() => Promise.resolve()));
    expect(screen.getAllByRole('button', { name: 'Drag to change the order' })).toHaveLength(2);
  });

  it('draws no grip for a lone row, nor for one on its way to the agent', () => {
    const onMove = vi.fn(() => Promise.resolve());
    rowsOf(
      waiting('a'),
      waiting('b', { steer: true, state: { k: 'in-input' } as QueueRow['state'] }),
    )(onMove);
    expect(screen.queryAllByRole('button', { name: 'Drag to change the order' })).toHaveLength(0);
  });

  it('a grip is picked up as the queue\u2019s own kind, carrying its whole row', () => {
    rowsOf(waiting('a'), waiting('b'))(vi.fn(() => Promise.resolve()));
    const [grip] = screen.getAllByRole('button', { name: 'Drag to change the order' });
    expect(grip.getAttribute('data-drag')).toBe('queued');
    expect(grip.getAttribute('data-drag-id')).toBe('a');
    expect(grip.getAttribute('data-drag-box')).toBe('drop');
  });

  it('↓ on a grip moves the row one place down, ↑ one up, and the ends of the line do not move', async () => {
    const onMove = vi.fn(() => Promise.resolve());
    rowsOf(waiting('a'), waiting('b'), waiting('c'))(onMove);
    const grips = screen.getAllByRole('button', { name: 'Drag to change the order' });
    fireEvent.keyDown(grips[0], { key: 'ArrowDown' });
    expect(onMove).toHaveBeenLastCalledWith('a', 'c');
    // Until the runtime has answered, nothing else is picked up.
    expect(grips[2].hasAttribute('data-drag')).toBe(false);
    await waitFor(() => expect(grips[2].hasAttribute('data-drag')).toBe(true));
    fireEvent.keyDown(grips[2], { key: 'ArrowUp', altKey: true });
    expect(onMove).toHaveBeenLastCalledWith('c', 'b');
    onMove.mockClear();
    fireEvent.keyDown(grips[0], { key: 'ArrowUp' });
    fireEvent.keyDown(grips[2], { key: 'ArrowDown' });
    expect(onMove).not.toHaveBeenCalled();
  });
});

describe('Queue rows carried by the grip', () => {
  const waiting = (clientId: string): QueueRow => ({
    clientId,
    text: `message ${clientId}`,
    attachments: [],
    state: { k: 'queued', position: 1, reason: 'behind' },
  });
  const pointer = (type: string, target: EventTarget, x: number, y: number) => {
    const event = new MouseEvent(type, {
      bubbles: true,
      cancelable: true,
      clientX: x,
      clientY: y,
      button: 0,
    });
    Object.assign(event, { pointerId: 1, pointerType: 'mouse' });
    act(() => {
      target.dispatchEvent(event);
    });
  };
  const slotOf = (id: string) => document.querySelector<HTMLElement>(`[data-drop-id="${id}"]`) as HTMLElement;
  // A move is drawn on the next frame; a let-go row eases into its place before the move goes out.
  const frame = () => act(() => void vi.advanceTimersByTime(16));
  const settle = () => act(() => void vi.advanceTimersByTime(200));

  beforeEach(() => {
    vi.useFakeTimers();
  });
  // The click that ends a drag is eaten until the next task, and a drop waits a while for its order; no test hands
  // either on to the next one.
  afterEach(() => {
    act(() => void vi.advanceTimersByTime(3000));
    vi.useRealTimers();
  });

  const setup = (onMove = vi.fn(() => Promise.resolve())) => {
    render(
      <Queue
        rows={[waiting('a'), waiting('b'), waiting('c')]}
        paused={null}
        waitingLimit={false}
        onRemove={vi.fn()}
        onResume={vi.fn()}
        onRetry={vi.fn()}
        onMove={onMove}
      />,
    );
    for (const [i, id] of ['a', 'b', 'c'].entries()) {
      const top = 100 + i * 40;
      slotOf(id).getBoundingClientRect = () =>
        ({ top, bottom: top + 40, height: 40, left: 0, right: 300, width: 300, x: 0, y: top }) as DOMRect;
    }
    return { onMove, grip: screen.getAllByRole('button', { name: 'Drag to change the order' })[0] };
  };

  it('carries the row itself past the one it is over, which steps aside; the move goes out once it has settled', () => {
    const { onMove, grip } = setup();
    pointer('pointerdown', grip, 10, 110);
    pointer('pointermove', window, 10, 170);
    frame();
    expect(slotOf('a').getAttribute('data-dragging')).toBe('true');
    expect(slotOf('a').style.transform).toBe('translate3d(0, 60px, 0)');
    expect(slotOf('b').style.transform).toBe('translate3d(0, -40px, 0)');
    expect(slotOf('c').style.transform).toBe('');
    expect(onMove).not.toHaveBeenCalled();
    pointer('pointerup', window, 10, 170);
    expect(slotOf('a').style.transform).toBe('translate3d(0, 40px, 0)');
    settle();
    expect(onMove).toHaveBeenCalledWith('a', 'c');
  });

  it('puts the row last when it is carried past the middle of the last one', () => {
    const { onMove, grip } = setup();
    pointer('pointerdown', grip, 10, 110);
    pointer('pointermove', window, 10, 205);
    pointer('pointerup', window, 10, 205);
    settle();
    expect(onMove).toHaveBeenCalledWith('a', null);
  });

  it('moves nothing and nobody steps aside while the row stays over its own place', () => {
    const { onMove, grip } = setup();
    pointer('pointerdown', grip, 10, 110);
    pointer('pointermove', window, 10, 130);
    frame();
    expect(slotOf('b').style.transform).toBe('');
    pointer('pointerup', window, 10, 130);
    settle();
    expect(onMove).not.toHaveBeenCalled();
    expect(slotOf('a').style.transform).toBe('');
  });

  it('Escape eases the row back where it was', () => {
    const { onMove, grip } = setup();
    pointer('pointerdown', grip, 10, 110);
    pointer('pointermove', window, 10, 170);
    fireEvent.keyDown(window, { key: 'Escape' });
    pointer('pointerup', window, 10, 170);
    settle();
    expect(onMove).not.toHaveBeenCalled();
    expect(slotOf('a').hasAttribute('data-dragging')).toBe(false);
    expect(slotOf('a').style.transform).toBe('');
    expect(slotOf('b').style.transform).toBe('');
  });
});

describe('Composer limits chip', () => {
  const left = {
    state: 'ready',
    provider: 'claude',
    limits: {
      fiveHour: { usedPct: 40, resetsAt: Date.now() / 1000 + 7200, forecast: null },
      sevenDay: null,
      updatedAt: null,
      plan: null,
      error: null,
    },
  } as const;

  it('puts what is left of the agent beside the model pill, and nothing before the machine answers', () => {
    const { rerender } = render(<Composer {...props({ mode: 'idle' })} />);
    expect(screen.queryByRole('button', { name: /limits/ })).toBeNull();
    rerender(<Composer {...props({ mode: 'idle', limits: left })} />);
    expect(screen.getByRole('button', { name: /Claude limits/ }).textContent).toContain('60%');
  });
});

describe('Composer files', () => {
  it('offers a retry on a file that did not upload', () => {
    const onRetryAttachment = vi.fn();
    const file = {
      id: 'up-1',
      name: 'shot.png',
      kind: 'image',
      status: 'error',
      error: 'Машина не ответила вовремя',
    } as const;
    render(<Composer {...props({ attachments: [file], onRetryAttachment })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Retry shot.png' }));
    expect(onRetryAttachment).toHaveBeenCalledWith('up-1');
  });
});

describe('Composer field height', () => {
  it('shrinks back to the text when it is cleared while the grow animation still holds the field', () => {
    // A layout model of the field: the text's own height, and the height a running WAAPI animation holds it at (an
    // animation outranks the inline height, so scrollHeight cannot read below it while it runs).
    const lineOf = (text: string) => Math.max(26, Math.ceil(text.length / 40) * 26);
    let held: number | null = null;
    const started: { from: string; to: string; cancel: ReturnType<typeof vi.fn> }[] = [];
    const animate = vi.fn(function (this: HTMLTextAreaElement, frames: Keyframe[]) {
      const from = String(frames[0].height);
      const to = String(frames[1].height);
      // Caught halfway: the field shows the height between the two.
      held = Math.round((parseInt(from, 10) + parseInt(to, 10)) / 2);
      const cancel = vi.fn(() => {
        held = null;
      });
      started.push({ from, to, cancel });
      return { cancel, playState: 'running' } as unknown as Animation;
    });
    const real = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'animate');
    Object.defineProperty(HTMLTextAreaElement.prototype, 'animate', { configurable: true, value: animate });
    const scroll = vi
      .spyOn(HTMLTextAreaElement.prototype, 'scrollHeight', 'get')
      .mockImplementation(function (this: HTMLTextAreaElement) {
        return Math.max(lineOf(this.value), held ?? 0);
      });
    const offset = vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (
      this: HTMLElement,
    ) {
      return held ?? (parseInt(this.style.height, 10) || 0);
    });
    try {
      const long = 'слово '.repeat(60);
      const view = render(<Composer {...props({ mode: 'working', value: '' })} />);
      const field = screen.getByRole('textbox') as HTMLTextAreaElement;
      expect(field.style.height).toBe('26px');
      // Dictation puts a long text in: the field eases up from one line.
      view.rerender(<Composer {...props({ mode: 'working', value: long })} />);
      expect(field.style.height).toBe(`${lineOf(long)}px`);
      expect(started.at(-1)).toMatchObject({ from: '26px', to: `${lineOf(long)}px` });
      // Sent into the queue a moment later, mid-ease: the running ease is dropped, the field measures its empty text
      // and eases down from where it stood to one line.
      const mid = Math.round((26 + lineOf(long)) / 2);
      view.rerender(<Composer {...props({ mode: 'working', value: '' })} />);
      expect(started[0].cancel).toHaveBeenCalled();
      expect(field.style.height).toBe('26px');
      expect(started).toHaveLength(2);
      expect(started[1]).toMatchObject({ from: `${mid}px`, to: '26px' });
    } finally {
      scroll.mockRestore();
      offset.mockRestore();
      if (real) Object.defineProperty(HTMLTextAreaElement.prototype, 'animate', real);
      else delete (HTMLTextAreaElement.prototype as { animate?: unknown }).animate;
    }
  });

  it('recalculates the height when the field changes width, not only when the text changes', () => {
    let notify: () => void = () => undefined;
    let width = 120;
    let height = 400;
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(cb: () => void) {
          notify = cb;
        }
        observe() {}
        disconnect() {}
      },
    );
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    });
    const widthSpy = vi
      .spyOn(HTMLTextAreaElement.prototype, 'clientWidth', 'get')
      .mockImplementation(() => width);
    const heightSpy = vi
      .spyOn(HTMLTextAreaElement.prototype, 'scrollHeight', 'get')
      .mockImplementation(() => height);
    try {
      render(<Composer {...props({ mode: 'idle', value: '' })} />);
      const field = screen.getByRole('textbox') as HTMLTextAreaElement;
      expect(field.style.height).toBe('400px');
      width = 480;
      height = 44;
      act(() => notify());
      expect(field.style.height).toBe('44px');
    } finally {
      widthSpy.mockRestore();
      heightSpy.mockRestore();
      vi.unstubAllGlobals();
    }
  });
});

describe('Composer with attached places', () => {
  const refs = [{ kind: 'task', id: 'ABC-1', title: 'ABC-1' }] as const;

  it('sends places alone, and each has its ×', () => {
    const onSubmit = vi.fn();
    const onRemoveRef = vi.fn();
    render(<Composer {...props({ value: '', mode: 'idle', onSubmit, refs: [...refs], onRemoveRef })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(onSubmit).toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Remove ABC-1' }));
    expect(onRemoveRef).toHaveBeenCalledWith(expect.objectContaining({ id: 'ABC-1' }));
  });
});
