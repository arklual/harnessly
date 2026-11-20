// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IVoiceState } from '@/next/features/voice-live/api';
import { pinLocale } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

const voice = vi.hoisted(() => ({ state: null as Partial<IVoiceState> | null, save: vi.fn() }));
vi.mock('@/next/features/voice-live/use-voice-state', () => ({
  useVoiceState: () => [voice.state, () => undefined],
}));
vi.mock('@/next/features/voice-live/api', async (original) => ({
  ...(await original<typeof import('@/next/features/voice-live/api')>()),
  saveVoiceSettings: voice.save,
}));
const nav = vi.hoisted(() => ({ goTo: vi.fn() }));
vi.mock('@/next/routes/routes', () => ({ goTo: nav.goTo, ROUTE: { settings: 'settings' } }));

const { default: VoiceQuick, QUICK_KIND } = await import('@/next/features/voice-quick/voice-quick');
const { LONG_PRESS_MS } = await import('@/next/features/voice-quick/use-quick');

const M = 'm1' as MachineId;

const desktop = (phone: boolean) => {
  window.matchMedia = ((query: string) => ({
    matches: phone,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
};

interface IDevice {
  deviceId: string;
  label: string;
  kind: 'audioinput';
}

const media = (devices: IDevice[]) => {
  const stop = vi.fn();
  const listed = { devices };
  const getUserMedia = vi.fn().mockImplementation(() => {
    // The page was allowed: from now on the devices have their names.
    listed.devices = [
      { deviceId: 'a', label: 'Built-in microphone', kind: 'audioinput' },
      { deviceId: 'b', label: 'USB headset', kind: 'audioinput' },
    ];
    return Promise.resolve({ getTracks: () => [{ stop }] });
  });
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      enumerateDevices: () => Promise.resolve(listed.devices),
      getUserMedia,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    },
  });
  return { getUserMedia, stop };
};

const NAMED: IDevice[] = [
  { deviceId: 'a', label: 'Built-in microphone', kind: 'audioinput' },
  { deviceId: 'b', label: 'USB headset', kind: 'audioinput' },
];
const HIDDEN: IDevice[] = [{ deviceId: '', label: '', kind: 'audioinput' }];

const Harness = ({ kind, onClick }: { kind: 'dictate' | 'live'; onClick: () => void }) => (
  <div>
    <VoiceQuick kind={kind} machine={M}>
      {({ note, ref, ...gestures }) => (
        <button {...gestures} ref={ref} type="button" aria-label="Mic" title={note} onClick={onClick}>
          mic
        </button>
      )}
    </VoiceQuick>
    <p>outside</p>
  </div>
);

const button = () => screen.getByRole('button', { name: 'Mic' });

beforeAll(() => pinLocale('en'));
beforeEach(() => {
  desktop(false);
  window.localStorage.clear();
  media(NAMED);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  voice.state = null;
  voice.save.mockReset();
  nav.goTo.mockReset();
});

describe('the quick settings of the mic (right click)', () => {
  it('opens over the button on a right click, without the browser menu, and the tip says how', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    expect(button().getAttribute('title')).toBe('Right-click for settings');
    expect(button().getAttribute('aria-haspopup')).toBe('dialog');
    expect(button().getAttribute('aria-expanded')).toBe('false');
    const notCancelled = fireEvent.contextMenu(button());
    expect(notCancelled).toBe(false);
    const panel = await screen.findByRole('dialog', { name: 'Dictation settings' });
    expect(button().getAttribute('aria-expanded')).toBe('true');
    // Dictation: the microphone, the language and the model; nothing of the voice mode.
    expect(screen.getByLabelText('Microphone')).toBeTruthy();
    expect(screen.getByLabelText('Dictation language')).toBeTruthy();
    expect(screen.getByLabelText('Model')).toBeTruthy();
    expect(screen.queryByRole('switch')).toBeNull();
    await waitFor(() => expect(panel.contains(document.activeElement)).toBe(true));
  });

  it('closes on Esc and gives the focus back to the button', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    button().focus();
    fireEvent.contextMenu(button());
    const panel = await screen.findByRole('dialog');
    await waitFor(() => expect(panel.contains(document.activeElement)).toBe(true));
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(document.activeElement).toBe(button());
  });

  it('closes on a click outside and on a second right click', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    await screen.findByRole('dialog');
    fireEvent.pointerDown(screen.getByText('outside'));
    fireEvent.mouseDown(screen.getByText('outside'));
    fireEvent.click(screen.getByText('outside'));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

    fireEvent.contextMenu(button());
    await screen.findByRole('dialog');
    // The press of the second right click lands on the button first: the panel is not closed by it and opened again.
    fireEvent.pointerDown(button(), { pointerType: 'mouse', button: 2 });
    fireEvent.mouseDown(button(), { button: 2 });
    fireEvent.contextMenu(button());
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it('opens from the keyboard: Shift+F10 and the menu key', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    button().focus();
    fireEvent.keyDown(button(), { key: 'F10', shiftKey: true });
    await screen.findByRole('dialog');
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    fireEvent.keyDown(button(), { key: 'ContextMenu' });
    // The browser sends its contextmenu event right after the key: one press, one toggle.
    fireEvent.contextMenu(button());
    await screen.findByRole('dialog');
  });

  it('leaves a plain click to the button', () => {
    const onClick = vi.fn();
    render(<Harness kind={QUICK_KIND.dictate} onClick={onClick} />);
    fireEvent.click(button());
    expect(onClick).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('picks the microphone: the choice is the one Settings keeps', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    const select = (await screen.findByLabelText('Microphone')) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option').length).toBe(3));
    fireEvent.change(select, { target: { value: 'b' } });
    expect(JSON.parse(window.localStorage.getItem('hl:voice') ?? '{}').deviceId).toBe('b');
    expect(screen.getByRole('img', { name: 'Microphone level' })).toBeTruthy();
  });

  it('offers «Allow access» while the devices have no names, and lists them once it was given', async () => {
    const m = media(HIDDEN);
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    const allow = await screen.findByRole('button', { name: 'Allow access' });
    expect(screen.queryByRole('img', { name: 'Microphone level' })).toBeNull();
    await act(async () => {
      fireEvent.click(allow);
    });
    expect(m.getUserMedia).toHaveBeenCalledWith({ audio: true });
    expect(m.stop).toHaveBeenCalled();
    const select = (await screen.findByLabelText('Microphone')) as HTMLSelectElement;
    expect(select.textContent).toContain('USB headset');
  });

  it('says so when the browser refuses the microphone', async () => {
    const m = media(HIDDEN);
    m.getUserMedia.mockRejectedValueOnce(new Error('denied'));
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    await act(async () => {
      fireEvent.click(await screen.findByRole('button', { name: 'Allow access' }));
    });
    expect(await screen.findByRole('status')).toBeTruthy();
  });

  it('changes the language and leads to all the voice settings', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    fireEvent.change(await screen.findByLabelText('Dictation language'), { target: { value: 'en' } });
    expect(JSON.parse(window.localStorage.getItem('hl:voice') ?? '{}').lang).toBe('en');
    fireEvent.click(screen.getByRole('button', { name: 'All voice settings' }));
    expect(nav.goTo).toHaveBeenCalledWith({ kind: 'settings', section: 'voice' });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
});

describe('the quick settings by a long press', () => {
  const touch = (type: 'pointerDown' | 'pointerUp' | 'pointerMove', x = 10, y = 10) =>
    fireEvent[type](button(), { pointerType: 'touch', isPrimary: true, clientX: x, clientY: y });

  it('opens after ~450 ms with a light buzz, and the click that ends the press does nothing', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    const buzz = vi.fn();
    Object.defineProperty(navigator, 'vibrate', { configurable: true, value: buzz });
    const onClick = vi.fn();
    render(<Harness kind={QUICK_KIND.dictate} onClick={onClick} />);
    touch('pointerDown');
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS - 10);
    });
    expect(button().getAttribute('aria-expanded')).toBe('false');
    act(() => {
      vi.advanceTimersByTime(20);
    });
    expect(button().getAttribute('aria-expanded')).toBe('true');
    expect(buzz).toHaveBeenCalledTimes(1);
    touch('pointerUp');
    fireEvent.click(button());
    // Android sends contextmenu after a long press as well: not a second press that would close the panel.
    fireEvent.contextMenu(button());
    expect(onClick).not.toHaveBeenCalled();
    expect(button().getAttribute('aria-expanded')).toBe('true');
    vi.useRealTimers();
    await screen.findByRole('dialog');
  });

  it('is a tap when the finger lifts early or wanders off', () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
    const onClick = vi.fn();
    render(<Harness kind={QUICK_KIND.dictate} onClick={onClick} />);
    touch('pointerDown');
    act(() => {
      vi.advanceTimersByTime(200);
    });
    touch('pointerUp');
    fireEvent.click(button());
    expect(onClick).toHaveBeenCalledTimes(1);
    touch('pointerDown');
    touch('pointerMove', 10, 60);
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS + 50);
    });
    expect(button().getAttribute('aria-expanded')).toBe('false');
  });

  it('is a bottom sheet on the phone', async () => {
    desktop(true);
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    const sheet = await screen.findByRole('dialog', { name: 'Dictation settings' });
    expect(sheet.classList.contains('hx-sheet')).toBe(true);
  });
});

describe('the quick settings of the wave', () => {
  it('has the voice of the answer and the barge-in switch, saved as the machine keeps them', async () => {
    voice.state = {
      ready: true,
      subscription: true,
      voice: 'cove',
      voices: ['cove', 'marin'],
      barge: true,
      talk: 'text',
    };
    voice.save.mockResolvedValue(voice.state);
    render(<Harness kind={QUICK_KIND.live} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    await screen.findByRole('dialog', { name: 'Voice mode settings' });
    expect(screen.getByLabelText('Microphone')).toBeTruthy();
    expect(screen.queryByLabelText('Dictation language')).toBeNull();
    fireEvent.change(screen.getByLabelText('Voice'), { target: { value: 'marin' } });
    expect(voice.save).toHaveBeenCalledWith(M, expect.objectContaining({ voice: 'marin', noBarge: false }));
    const barge = screen.getByRole('switch', { name: 'Interrupt by voice' });
    await waitFor(() => expect((barge as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(barge);
    expect(voice.save).toHaveBeenLastCalledWith(M, expect.objectContaining({ noBarge: true }));
    fireEvent.click(screen.getByRole('button', { name: 'All voice settings' }));
    expect(nav.goTo).toHaveBeenCalledWith({ kind: 'settings', section: 'accounts' });
  });

  it('says what to do while the voice mode is off, and keeps the microphone', async () => {
    voice.state = { ready: false, subscription: false };
    render(<Harness kind={QUICK_KIND.live} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    await screen.findByRole('dialog');
    expect(screen.getByLabelText('Microphone')).toBeTruthy();
    expect(screen.queryByRole('switch')).toBeNull();
    expect(screen.getByText(/The voice mode is off/)).toBeTruthy();
  });
});
