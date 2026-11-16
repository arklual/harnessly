// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { IVoiceState } from '@/next/features/voice-live/api';
import type { MachineId } from '@/next/protocol/v2';

const voice = vi.hoisted(() => ({ state: null as Partial<IVoiceState> | null }));
vi.mock('@/next/features/voice-live/use-voice-state', () => ({
  useVoiceState: () => [voice.state, () => undefined],
  voiceOffered: (st: IVoiceState | null) => !!(st?.ready || st?.subscription),
}));
const nav = vi.hoisted(() => ({ goTo: vi.fn() }));
vi.mock('@/next/routes/routes', () => ({ goTo: nav.goTo, ROUTE: { settings: 'settings' } }));

const { default: VoiceStart } = await import('@/next/features/voice-live/voice-start');

const M = 'm1' as MachineId;

afterEach(() => {
  cleanup();
  voice.state = null;
  nav.goTo.mockReset();
});

describe('the wave of the new chat (HRN-1518)', () => {
  it('stands nowhere until the state is read', () => {
    render(<VoiceStart machine={M} onStart={() => undefined} />);
    expect(document.querySelector('.hx-voice-btn')).toBeNull();
  });

  it('starts the talk when the voice is ready', () => {
    voice.state = { ready: true, subscription: true };
    const onStart = vi.fn();
    render(<VoiceStart machine={M} onStart={onStart} />);
    const btn = document.querySelector('.hx-voice-btn') as HTMLButtonElement;
    expect(btn.hasAttribute('data-off')).toBe(false);
    fireEvent.click(btn);
    expect(onStart).toHaveBeenCalled();
  });

  it('stays, quiet, with what to do when the mode is off — and leads to the settings', () => {
    voice.state = { ready: false, subscription: false };
    const onStart = vi.fn();
    render(<VoiceStart machine={M} onStart={onStart} />);
    const btn = document.querySelector('.hx-voice-btn') as HTMLButtonElement;
    expect(btn.getAttribute('aria-disabled')).toBe('true');
    expect(btn.getAttribute('aria-label')).toMatch(/Settings/);
    fireEvent.click(btn);
    expect(onStart).not.toHaveBeenCalled();
    expect(nav.goTo).toHaveBeenCalledWith({ kind: 'settings', section: 'accounts' });
  });

  it('asks for the ChatGPT sign-in when the subscription route has no login', () => {
    voice.state = { ready: false, subscription: true };
    render(<VoiceStart machine={M} onStart={() => undefined} />);
    expect(screen.getByRole('button').getAttribute('aria-label')).toMatch(/Sign in with ChatGPT/);
  });
});
