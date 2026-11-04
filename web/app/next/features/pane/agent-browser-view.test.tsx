// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AgentBrowserView from './agent-browser-view';
import type { IBrowserTab } from './agent-browser-frame';
vi.mock('@/next/i18n', () => ({ t: (key: string) => key.split('.').at(-1) }));
class Socket {
  static OPEN = 1;
  static last: Socket;
  readyState = 1;
  binaryType = '';
  onmessage: ((e: { data: string | ArrayBuffer }) => void) | null = null;
  onclose: (() => void) | null = null;
  sent: Record<string, unknown>[] = [];
  constructor() {
    Socket.last = this;
  }
  send(s: string) {
    this.sent.push(JSON.parse(s) as Record<string, unknown>);
  }
  close() {}
  emit(value: unknown) {
    act(() => this.onmessage?.({ data: JSON.stringify(value) }));
  }
}
const tab = (id: string, owner: 'this' | 'other' = 'this'): IBrowserTab => ({
  id,
  title: id,
  url: 'about:blank',
  owner,
  by: 'agent',
  viewport: { mode: 'fixed', w: 1280, h: 800, dpr: 1 },
  crashed: false,
  control: 'agent',
  agentAt: 0,
});
const tabs = (items: IBrowserTab[], current = items[0]?.id ?? '', following = false) =>
  Socket.last.emit({ t: 'tabs', tabs: items, current, following, gen: 1, agentTab: current });
beforeEach(() => {
  vi.stubGlobal('WebSocket', Socket);
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
  vi.useRealTimers();
});
describe('agent browser panel', () => {
  it('keeps server order, one-tab strip, collapsed other chats, close and follow commands', () => {
    render(<AgentBrowserView url="ws://test" />);
    tabs([tab('b'), tab('a'), tab('other', 'other')]);
    expect(screen.getAllByRole('tab').map((el) => el.textContent)).toEqual(['b', 'a', 'other']);
    expect(document.querySelector('details')?.open).toBe(false);
    fireEvent.click(screen.getByRole('tab', { name: 'a' }));
    fireEvent.click(screen.getByLabelText('closeTab: b'));
    fireEvent.click(screen.getByText('follow'));
    expect(Socket.last.sent).toEqual(
      expect.arrayContaining([{ t: 'tab.select', id: 'a' }, { t: 'tab.close', id: 'b' }, { t: 'follow' }]),
    );
    tabs([tab('b')]);
    expect(screen.getAllByRole('tab')).toHaveLength(1);
  });
  it('shows preparing, server errors and recoverable notices', () => {
    render(<AgentBrowserView url="ws://test" />);
    Socket.last.emit({
      t: 'status',
      state: 'preparing',
      progress: 35,
      hint: 'Unpacking',
      profile: { id: 'common', name: 'Common' },
    });
    expect(screen.getByRole('progressbar').getAttribute('value')).toBe('35');
    expect(screen.getByText('Unpacking')).toBeTruthy();
    for (const state of ['starting', 'up', 'down', 'error']) {
      Socket.last.emit({
        t: 'status',
        state,
        hint: 'Renderer diagnostics',
        profile: { id: 'common', name: 'Common' },
      });
      expect(screen.queryByText('Renderer diagnostics')).toBeNull();
    }
    expect(screen.queryByText('Unpacking')).toBeNull();
    Socket.last.emit({ t: 'notice', message: 'Try again' });
    expect(screen.getByRole('alert').textContent).toBe('noticeFailed');
    Socket.last.emit({
      t: 'status',
      state: 'error',
      error: 'Chrome could not start',
      profile: { id: 'common', name: 'Common' },
    });
    expect(screen.getByText('Chrome could not start')).toBeTruthy();
  });
  it('returns control, answers prompts with empty text and chooses select indices', () => {
    render(<AgentBrowserView url="ws://test" />);
    tabs([
      { ...tab('a'), control: 'person', dialog: { type: 'prompt', message: 'Name?', defaultPrompt: 'old' } },
    ]);
    fireEvent.change(screen.getByLabelText('answer'), { target: { value: '' } });
    fireEvent.submit(screen.getByRole('dialog'));
    fireEvent.click(screen.getByText('release'));
    expect(Socket.last.sent).toEqual(
      expect.arrayContaining([
        { t: 'dialog', accept: true, text: '' },
        { t: 'control', holder: 'agent' },
      ]),
    );
    tabs([tab('a')]);
    Socket.last.emit({
      t: 'select',
      x: 0,
      y: 0,
      w: 100,
      h: 20,
      selected: 0,
      options: [
        { value: 'same', label: 'One', disabled: false },
        { value: 'same', label: 'Two', disabled: false },
      ],
    });
    fireEvent.click(screen.getByRole('option', { name: 'Two' }));
    expect(Socket.last.sent.at(-1)).toEqual({ t: 'select.choose', index: 1 });
  });
});

describe('viewport and frame timing', () => {
  it('debounces fit, respects the minimum size and leaves a fixed viewport alone', () => {
    vi.useFakeTimers();
    let measure: (w: number, h: number) => void = () => undefined;
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(callback: ResizeObserverCallback) {
          measure = (w, h) =>
            callback(
              [{ contentRect: { width: w, height: h } } as ResizeObserverEntry],
              this as unknown as ResizeObserver,
            );
        }
        observe() {}
        disconnect() {}
      },
    );
    render(<AgentBrowserView url="ws://test" />);
    Socket.last.emit({ t: 'status', state: 'up', profile: { id: 'common', name: 'Common' } });
    tabs([{ ...tab('a'), viewport: { mode: 'fit', w: 800, h: 600, dpr: 1 } }]);
    act(() => {
      measure(100, 100);
      vi.advanceTimersByTime(200);
    });
    expect(Socket.last.sent).toHaveLength(0);
    act(() => vi.advanceTimersByTime(300));
    expect(Socket.last.sent.at(-1)).toEqual({ t: 'viewport', mode: 'fit', w: 320, h: 240, dpr: 1 });
    tabs([tab('a')]);
    Socket.last.sent = [];
    act(() => {
      measure(700, 500);
      vi.advanceTimersByTime(500);
    });
    expect(Socket.last.sent).toHaveLength(0);
  });
  it('closes a bitmap decoded after its tab was replaced without displaying it', async () => {
    let finish: (image: ImageBitmap) => void = () => undefined;
    vi.stubGlobal(
      'createImageBitmap',
      vi.fn(
        () =>
          new Promise<ImageBitmap>((resolve) => {
            finish = resolve;
          }),
      ),
    );
    render(<AgentBrowserView url="ws://test" />);
    tabs([tab('a')]);
    const frame = new ArrayBuffer(13),
      view = new DataView(frame);
    view.setUint32(0, 800);
    view.setUint32(4, 600);
    view.setUint32(8, 1);
    act(() => Socket.last.onmessage?.({ data: frame }));
    Socket.last.emit({ t: 'tabs', tabs: [tab('b')], current: 'b', following: false, agentTab: 'b', gen: 2 });
    const close = vi.fn();
    await act(async () => {
      finish({ close, width: 800, height: 600 } as unknown as ImageBitmap);
      await Promise.resolve();
    });
    expect(close).toHaveBeenCalledOnce();
    expect(screen.getByLabelText('screen').getAttribute('data-shown')).toBe('false');
  });
});

describe('panel review fixes', () => {
  it('does not fit another conversation or a legacy agent viewport', () => {
    vi.useFakeTimers();
    render(<AgentBrowserView url="ws://test" />);
    Socket.last.emit({ t: 'status', state: 'up', profile: { id: 'common', name: 'Common' } });
    tabs([{ ...tab('other', 'other'), viewport: { mode: 'fit', w: 800, h: 600, dpr: 1 } }]);
    act(() => vi.advanceTimersByTime(600));
    expect(Socket.last.sent).toHaveLength(0);
    tabs([{ ...tab('agent'), viewport: { mode: '' as 'fixed', w: 1280, h: 800, dpr: 1 } }]);
    act(() => vi.advanceTimersByTime(600));
    expect(Socket.last.sent).toHaveLength(0);
    expect(screen.getByLabelText('resize')).toBeTruthy();
  });
  it('expires translated notices and offers a retry with the repair command', () => {
    vi.useFakeTimers();
    vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    render(<AgentBrowserView url="ws://test" />);
    Socket.last.emit({ t: 'notice', message: 'Not attached to an active page' });
    expect(screen.getByRole('alert').textContent).toBe('noticePage');
    act(() => vi.advanceTimersByTime(5100));
    expect(screen.queryByRole('alert')).toBeNull();
    Socket.last.emit({
      t: 'status',
      state: 'error',
      error: 'Setup failed',
      command: 'browser install',
      profile: { id: 'p', name: 'P' },
    });
    expect(screen.getByText('browser install')).toBeTruthy();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    fireEvent.click(screen.getByText('copy'));
    expect(writeText).toHaveBeenCalledWith('browser install');
    fireEvent.click(screen.getByText('retry'));
    expect(Socket.last.sent.at(-1)).toEqual({ t: 'browser.retry' });
    vi.restoreAllMocks();
  });
  it('clears keyboard text immediately and forwards editing keys', () => {
    render(<AgentBrowserView url="ws://test" />);
    const input = screen.getByLabelText('keyboardInput') as HTMLTextAreaElement;
    fireEvent.input(input, { target: { value: 'private' } });
    expect(input.value).toBe('');
    expect(Socket.last.sent.at(-1)).toEqual({ t: 'text', text: 'private' });
    fireEvent.keyDown(input, { key: 'Backspace', code: 'Backspace' });
    expect(Socket.last.sent.at(-1)).toEqual({ t: 'key', key: 'Backspace', code: 'Backspace', keyCode: 8 });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
    expect(Socket.last.sent.at(-1)).toEqual({ t: 'key', key: 'Enter', code: 'Enter', keyCode: 13 });
  });
});

describe('scripted page warning', () => {
  it('warns when the person takes over a scripted page and keeps the warning through navigation', () => {
    render(<AgentBrowserView url="ws://test" />);
    tabs([{ ...tab('a'), scripted: true }]);
    expect(screen.queryByText('scriptedWarning')).toBeNull();
    fireEvent.click(screen.getByText('take'));
    expect(Socket.last.sent.at(-1)).toEqual({ t: 'control', holder: 'person' });
    tabs([{ ...tab('a'), scripted: true, control: 'person' }]);
    expect(screen.getByRole('alert').textContent).toBe('scriptedWarning');
    tabs([{ ...tab('a'), url: 'https://example.org/login', scripted: true, control: 'person' }]);
    expect(screen.getByRole('alert').textContent).toBe('scriptedWarning');
    tabs([{ ...tab('fresh'), by: 'person', scripted: false, control: 'person' }]);
    expect(screen.queryByText('scriptedWarning')).toBeNull();
  });

  it('leaves fresh manual login tabs warning-free, including older messages without a marker', () => {
    render(<AgentBrowserView url="ws://test" />);
    tabs([{ ...tab('login'), by: 'person', control: 'person' }]);
    expect(screen.getByText('personControl')).toBeTruthy();
    expect(screen.queryByText('scriptedWarning')).toBeNull();
    tabs(
      [
        { ...tab('other', 'other'), scripted: true },
        { ...tab('login'), by: 'person', control: 'person' },
      ],
      'login',
    );
    expect(screen.queryByText('scriptedWarning')).toBeNull();
  });
});
