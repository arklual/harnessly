// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IVoiceState } from '@/next/features/voice-live/api';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pinLocale } from '@/next/i18n';
import { useOnePopover } from '@/next/ui/one-popover';
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
    listed.devices = NAMED;
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
  { deviceId: 'a', label: 'MacBook Pro Microphone (Built-in)', kind: 'audioinput' },
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

const Other = () => {
  const [open, setOpen] = useState(false);
  useOnePopover(open, () => setOpen(false));
  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>
        Other
      </button>
      {open && <p>other is open</p>}
    </div>
  );
};

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

const row = (name: RegExp | string) => screen.findByRole('button', { name });

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
    // The card's head: the device as it is said, what it hears under it.
    await screen.findByRole('img', { name: 'Microphone level' });
    expect(screen.getAllByText('System default').length).toBeGreaterThan(0);
    expect(screen.getByRole('img', { name: 'Microphone level' })).toBeTruthy();
    // Dictation: the microphone, the language and the model; nothing of the voice mode. No native controls.
    expect(await row(/^Microphone/)).toBeTruthy();
    expect(screen.getByRole('button', { name: /^Language/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /^Model/ })).toBeTruthy();
    expect(panel.querySelector('select')).toBeNull();
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
    const panel = await screen.findByRole('dialog');
    await waitFor(() => expect(panel.contains(document.activeElement)).toBe(true));
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

  it('picks the microphone in a list in the same card: a tick at the current, «Back» returns', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    fireEvent.click(await row(/^Microphone/));
    const list = await screen.findByRole('radiogroup', { name: 'Microphone' });
    // «MacBook Pro» without «(Built-in)» and the «Microphone» word; the full name stays as the quiet line.
    const options = [...list.querySelectorAll('[role=radio]')];
    expect(options.map((o) => o.textContent)).toEqual([
      'System default',
      'MacBook ProMacBook Pro Microphone (Built-in)',
      'USB headset',
    ]);
    expect(options[0]?.getAttribute('aria-checked')).toBe('true');
    expect(screen.getByRole('dialog').querySelector('select')).toBeNull();
    fireEvent.click(options[2] as HTMLElement);
    expect(JSON.parse(window.localStorage.getItem('hl:voice') ?? '{}').deviceId).toBe('b');
    // The pick goes back to the settings, the focus to the row that opened the list, the device in the head.
    const back = await row(/^Microphone/);
    await waitFor(() => expect(document.activeElement).toBe(back));
    expect(back.textContent).toContain('USB headset');
    expect(screen.queryByRole('radiogroup')).toBeNull();
  });

  it('goes back from a list on «Back» and on Esc, and closes on the next Esc', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    fireEvent.click(await row(/^Language/));
    await screen.findByRole('radiogroup', { name: 'Language' });
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    await row(/^Language/);
    fireEvent.click(await row(/^Language/));
    await screen.findByRole('radiogroup', { name: 'Language' });
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    await row(/^Language/);
    expect(screen.getByRole('dialog')).toBeTruthy();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
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
    expect(await row(/^Microphone/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Allow access' })).toBeNull();
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
    fireEvent.click(await row(/^Language/));
    fireEvent.click(await screen.findByRole('radio', { name: 'English' }));
    expect(JSON.parse(window.localStorage.getItem('hl:voice') ?? '{}').lang).toBe('en');
    expect((await row(/^Language/)).textContent).toContain('English');
    fireEvent.click(screen.getByRole('button', { name: 'All voice settings' }));
    expect(nav.goTo).toHaveBeenCalledWith({ kind: 'settings', section: 'voice' });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it('names a model short and shows why one cannot be picked', async () => {
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    const model = await row(/^Model/);
    expect(model.textContent).toContain('On the machine');
    fireEvent.click(model);
    const list = await screen.findByRole('radiogroup', { name: 'Model' });
    const chatgpt = list.querySelectorAll<HTMLButtonElement>('[role=radio]')[1];
    expect(chatgpt?.disabled).toBe(true);
    expect(chatgpt?.textContent).toContain('Codex');
  });
});

describe('one card at a time', () => {
  it('closes when another popover of the field opens, and is closed by it', async () => {
    render(
      <>
        <Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />
        <Other />
      </>,
    );
    fireEvent.contextMenu(button());
    await screen.findByRole('dialog');
    fireEvent.click(screen.getByRole('button', { name: 'Other' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('other is open')).toBeTruthy();
    // And the other way round: this one opening closes the other.
    fireEvent.contextMenu(button());
    await screen.findByRole('dialog');
    await waitFor(() => expect(screen.queryByText('other is open')).toBeNull());
  });
});

describe('the quick settings by a long press', () => {
  const touch = (
    type: 'pointerDown' | 'pointerUp' | 'pointerMove',
    extra: Record<string, unknown> = {},
    at = { x: 10, y: 10 },
  ) =>
    fireEvent[type](button(), {
      pointerType: 'touch',
      isPrimary: true,
      clientX: at.x,
      clientY: at.y,
      ...extra,
    });
  const fake = () => vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });

  it('opens after ~450 ms with a light buzz, and the click that ends the press does nothing', async () => {
    fake();
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
    // Android sends contextmenu after a long press as well, and takes it for its own menu unless it is cancelled: it is
    // cancelled, and it is not a second press that would close the panel.
    expect(fireEvent.contextMenu(button())).toBe(false);
    expect(onClick).not.toHaveBeenCalled();
    expect(button().getAttribute('aria-expanded')).toBe('true');
    vi.useRealTimers();
    await screen.findByRole('dialog');
  });

  it('works for a pen as for a finger, and does without a buzz where the browser has none (iOS Safari)', () => {
    fake();
    Object.defineProperty(navigator, 'vibrate', { configurable: true, value: undefined });
    render(<Harness kind={QUICK_KIND.live} onClick={vi.fn()} />);
    touch('pointerDown', { pointerType: 'pen' });
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS + 10);
    });
    expect(button().getAttribute('aria-expanded')).toBe('true');
  });

  it('is a tap when the finger lifts early or wanders off', () => {
    fake();
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
    touch('pointerMove', {}, { x: 10, y: 60 });
    act(() => {
      vi.advanceTimersByTime(LONG_PRESS_MS + 50);
    });
    expect(button().getAttribute('aria-expanded')).toBe('false');
  });

  it('ignores a second finger and a mouse press held down', () => {
    fake();
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    touch('pointerDown', { isPrimary: false });
    fireEvent.pointerDown(button(), { pointerType: 'mouse', isPrimary: true });
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

  it('keeps iOS and Android from showing their own menu, loupe or selection over the buttons', () => {
    // No browser here draws a callout, so the rule that stops it is read from the stylesheet it ships in.
    const css = readFileSync(resolve(__dirname, '../../styles/voice-quick.css'), 'utf8');
    const rule = /\.hx-round\[data-quick\]\s*\{([^}]*)\}/.exec(css)?.[1] ?? '';
    expect(rule).toMatch(/-webkit-touch-callout:\s*none/);
    expect(rule).toMatch(/-webkit-user-select:\s*none/);
    expect(rule).toMatch(/(^|[\s;])user-select:\s*none/);
    expect(rule).toMatch(/touch-action:\s*manipulation/);
    render(<Harness kind={QUICK_KIND.dictate} onClick={vi.fn()} />);
    expect(button().hasAttribute('data-quick')).toBe(true);
  });
});

describe('the quick settings of the wave', () => {
  const ready = {
    ready: true,
    subscription: true,
    voice: 'cove',
    voices: ['cove', 'marin', 'sol'],
    barge: true,
    talk: 'text' as const,
  };

  it('has the voice of the answer in a list and the barge-in switch, saved as the machine keeps them', async () => {
    voice.state = ready;
    voice.save.mockResolvedValue(voice.state);
    render(<Harness kind={QUICK_KIND.live} onClick={vi.fn()} />);
    fireEvent.contextMenu(button());
    const panel = await screen.findByRole('dialog', { name: 'Voice mode settings' });
    expect(await row(/^Microphone/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Language/ })).toBeNull();
    expect(panel.querySelector('select')).toBeNull();
    const voiceRow = await row(/^Voice/);
    expect(voiceRow.textContent).toContain('Cove');
    fireEvent.click(voiceRow);
    const list = await screen.findByRole('radiogroup', { name: 'Voice' });
    const options = [...list.querySelectorAll('[role=radio]')];
    expect(options.map((o) => o.textContent)).toEqual(['Cove', 'Marin', 'Sol']);
    expect(options[0]?.getAttribute('aria-checked')).toBe('true');
    fireEvent.click(options[1] as HTMLElement);
    expect(voice.save).toHaveBeenCalledWith(M, expect.objectContaining({ voice: 'marin', noBarge: false }));
    const barge = await screen.findByRole('switch', { name: 'Interrupt by voice' });
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
    expect(await row(/^Microphone/)).toBeTruthy();
    expect(screen.queryByRole('switch')).toBeNull();
    expect(screen.getByText(/The voice mode is off/)).toBeTruthy();
  });
});
