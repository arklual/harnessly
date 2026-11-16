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
