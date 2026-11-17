// @vitest-environment jsdom
// The full-screen talk: the state's word, the buttons, Space for the microphone, the caption, folding.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import {
  FIELD_FLOOR,
  setVoiceExpanded,
  setVoiceField,
  voiceExpanded,
  type IVoiceField,
} from '@/next/features/voice-live/field';
import { setVoiceMood, voiceMood } from '@/next/features/voice-live/voice-mood';
import { showOnVoiceStage } from '@/next/features/voice-live/voice-stage';
import type { ConvId, MachineId } from '@/next/protocol/v2';

vi.mock('@/next/i18n', () => ({ t: (key: string) => key }));
vi.mock('@/next/features/voice-live/orb', () => ({ default: () => <canvas data-testid="orb" /> }));

import { VoiceFullBody } from '@/next/features/voice-live/voice-full';

const field = (patch: Partial<IVoiceField> = {}): IVoiceField => ({
  host: null,
  conv: 'c1' as ConvId,
  machine: 'm1' as MachineId,
  end: vi.fn(),
  floor: FIELD_FLOOR.listening,
  level: () => 0.3,
  heard: '',
  said: '',
  typed: vi.fn(),
  head: 'h',
  interrupted: false,
  muted: false,
  mute: vi.fn(),
  ...patch,
});

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    value: () => ({
      matches: false,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }),
  });
});
afterEach(() => {
  cleanup();
  setVoiceField(null);
  showOnVoiceStage(null);
  setVoiceMood('neutral');
});

describe('the full-screen talk', () => {
  it('says the state in one word, in a live region', () => {
    render(<VoiceFullBody voice={field({ floor: FIELD_FLOOR.thinking })} title="План" />);
    const status = screen.getByRole('status');
    expect(status.textContent).toBe('next.chatgpt.voice.full.s.thinking');
    expect(status.getAttribute('aria-live')).toBe('polite');
    expect(screen.getByText('План')).toBeTruthy();
  });

  it('turns the microphone by the button and by Space, but not by Space on a button', () => {
    const voice = field();
    render(<VoiceFullBody voice={voice} title={null} />);
    fireEvent.click(screen.getByText('next.chatgpt.voice.full.mute'));
    expect(voice.mute).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: ' ' });
    expect(voice.mute).toHaveBeenCalledTimes(2);
    // A focused button keeps its own Space (the click above is its one).
    fireEvent.keyDown(screen.getByText('next.chatgpt.voice.full.chat'), { key: ' ' });
    expect(voice.mute).toHaveBeenCalledTimes(2);
  });

  it('shows what is said as a caption and hides it by the button', () => {
    const { rerender } = render(
      <VoiceFullBody voice={field({ heard: 'расскажи мне про день' })} title={null} />,
    );
    expect(screen.getByText('расскажи мне про день')).toBeTruthy();
    // The phrase stays until the other side begins one.
    rerender(<VoiceFullBody voice={field({ heard: '' })} title={null} />);
    expect(screen.getByText('расскажи мне про день')).toBeTruthy();
    fireEvent.click(screen.getByLabelText('next.chatgpt.voice.full.subsOff'));
    expect(screen.queryByText('расскажи мне про день')).toBeNull();
  });

  it('folds by «Чат» and the chevron, ends by «Завершить»', () => {
    const voice = field();
    const fold = vi.fn();
    render(<VoiceFullBody voice={voice} title={null} onFold={fold} />);
    fireEvent.click(screen.getByText('next.chatgpt.voice.full.chat'));
    fireEvent.click(screen.getByLabelText('next.chatgpt.voice.full.fold'));
    expect(fold).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByText('next.chatgpt.voice.bar.end'));
    expect(voice.end).toHaveBeenCalledTimes(1);
  });

  it('folds by a pull down and springs back from a short one', () => {
    vi.useFakeTimers();
    const fold = vi.fn();
    render(<VoiceFullBody voice={field()} title={null} onFold={fold} />);
    const layer = screen.getByRole('dialog');
    layer.setPointerCapture = () => undefined;
    const at = (type: 'pointerDown' | 'pointerMove' | 'pointerUp', y: number) =>
      fireEvent[type](layer, { pointerId: 1, clientY: y, button: 0 });
    at('pointerDown', 100);
    at('pointerMove', 130);
    at('pointerUp', 130);
    expect(layer.style.transform).toBe('');
    expect(fold).not.toHaveBeenCalled();
    at('pointerDown', 100);
    at('pointerMove', 180);
    at('pointerMove', 320);
    at('pointerUp', 320);
    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(fold).toHaveBeenCalledTimes(1);
    expect(layer.dataset.gone).toBe('true');
    vi.useRealTimers();
  });
});

describe('the talk layer state', () => {
  it('opens with a talk, folds and opens by hand, and goes with the talk', () => {
    expect(voiceExpanded()).toBe(false);
    setVoiceExpanded(true);
    expect(voiceExpanded()).toBe(false);
    setVoiceField(field());
    expect(voiceExpanded()).toBe(true);
    setVoiceExpanded(false);
    // The field is set again as the call's state changes: the fold stays.
    setVoiceField(field({ floor: FIELD_FLOOR.speaking }));
    expect(voiceExpanded()).toBe(false);
    setVoiceExpanded(true);
    expect(voiceExpanded()).toBe(true);
    setVoiceField(null);
    expect(voiceExpanded()).toBe(false);
  });
});

describe('the mascot', () => {
  it('stands in the middle with the orb as its aura, and wears the mood', () => {
    const { container } = render(<VoiceFullBody voice={field()} title={null} />);
    expect(container.querySelector('.hx-voice-full__mascot[data-view="voice"] svg.hx-head')).toBeTruthy();
    expect(screen.getByTestId('orb')).toBeTruthy();
    const svg = container.querySelector('svg.hx-head');
    expect(svg?.getAttribute('data-m')).toBe('neutral');
    act(() => setVoiceMood('joy', 0));
    expect(svg?.getAttribute('data-m')).toBe('joy');
  });

  it('returns to neutral by itself after the time of the mood', () => {
    vi.useFakeTimers();
    setVoiceMood('surprised', 1000);
    expect(voiceMood()).toBe('surprised');
    vi.advanceTimersByTime(1001);
    expect(voiceMood()).toBe('neutral');
    // A new mood restarts the time.
    setVoiceMood('sad', 1000);
    vi.advanceTimersByTime(600);
    setVoiceMood('joy', 1000);
    vi.advanceTimersByTime(600);
    expect(voiceMood()).toBe('joy');
    vi.useRealTimers();
  });
});

describe('the stage', () => {
  it('takes the area with a title and a close button, the mascot steps down', () => {
    const { container } = render(<VoiceFullBody voice={field()} title={null} />);
    expect(container.querySelector('.hx-voice-full__panel')).toBeNull();
    act(() => showOnVoiceStage(<p>карточка</p>, 'Квоты'));
    expect(screen.getByText('карточка')).toBeTruthy();
    expect(screen.getByText('Квоты')).toBeTruthy();
    expect(container.querySelector('.hx-voice-full__mascot')?.getAttribute('data-view')).toBe('stage');
    // The orb is a voice-layout aura only.
    expect(screen.queryByTestId('orb')).toBeNull();
    act(() => {
      fireEvent.click(screen.getByLabelText('next.chatgpt.voice.full.stage.close'));
    });
    expect(container.querySelector('.hx-voice-full__mascot')?.getAttribute('data-view')).toBe('voice');
    expect(container.querySelector('.hx-voice-full__panel')?.hasAttribute('data-closing')).toBe(true);
  });

  it('goes away with the talk', () => {
    showOnVoiceStage(<p>x</p>);
    setVoiceField(field());
    setVoiceField(null);
    const { container } = render(<VoiceFullBody voice={field()} title={null} />);
    expect(container.querySelector('.hx-voice-full__panel')).toBeNull();
  });
});

describe('the text mode', () => {
  const turns = [
    { role: 'user' as const, text: 'привет', done: true },
    { role: 'assistant' as const, text: 'здравствуйте', done: true },
  ];

  it('shows the feed and a text field while the microphone is off, none while it is on', () => {
    const { rerender, container } = render(<VoiceFullBody voice={field({ turns })} title={null} />);
    expect(screen.queryByRole('log')).toBeNull();
    expect(screen.queryByRole('textbox')).toBeNull();
    rerender(<VoiceFullBody voice={field({ turns, muted: true })} title={null} />);
    expect(screen.getByRole('log').textContent).toContain('здравствуйте');
    expect(screen.getByText('привет').getAttribute('data-role')).toBe('user');
    expect(container.querySelector('.hx-voice-full__mascot')?.getAttribute('data-view')).toBe('text');
    // Unmuting brings the voice layout back (the feed leaves with an exit).
    rerender(<VoiceFullBody voice={field({ turns, muted: false })} title={null} />);
    expect(container.querySelector('.hx-voice-full__mascot')?.getAttribute('data-view')).toBe('voice');
  });

  it('sends by Enter into the call, keeps Shift+Enter for a new line, and sends nothing empty', () => {
    const voice = field({ muted: true, turns });
    render(<VoiceFullBody voice={voice} title={null} />);
    const input = screen.getByRole('textbox');
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(voice.typed).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: 'что по релизу?' } });
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: true });
    expect(voice.typed).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(voice.typed).toHaveBeenCalledWith('что по релизу?');
    expect((input as HTMLTextAreaElement).value).toBe('');
    // Space in the field is a space, not the microphone.
    fireEvent.keyDown(input, { key: ' ' });
    expect(voice.mute).not.toHaveBeenCalled();
  });
});
