// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { IVoiceState } from '@/next/features/voice-live/api';
import type { MachineId } from '@/next/protocol/v2';

vi.mock('@/next/i18n', () => ({ t: (key: string) => key }));
const toasts = vi.hoisted(() => ({ list: [] as { text: string; action?: string }[] }));
vi.mock('@/next/ui/toast', () => ({
  toast: (text: string, o: { action?: { label: string } } = {}) => {
    toasts.list.push({ text, action: o.action?.label });
    return () => undefined;
  },
}));
const state = vi.hoisted(() => ({ current: null as IVoiceState | null, failures: 0 }));
vi.mock('@/next/features/voice-live/api', async (orig) => ({
  ...(await orig<typeof import('@/next/features/voice-live/api')>()),
  loadVoiceState: () => {
    if (state.failures > 0) {
      state.failures -= 1;
      return Promise.reject(new Error('machine_offline'));
    }
    return Promise.resolve(state.current);
  },
}));

import VoiceLive from '@/next/features/voice-live/voice-live';
import { spokenResult } from '@/next/features/voice-live/spoken';
import { addPiece } from '@/next/features/voice-live/use-voice-call';
import { voiceStateRetryDelay } from '@/next/features/voice-live/api';

const base: IVoiceState = {
  subscription: true,
  signedIn: true,
  hasKey: false,
  route: 'subscription',
  ready: true,
  voice: 'cove',
  voices: ['cove'],
  model: 'gpt-live-1.1-codex',
  models: ['gpt-live-1.1-codex'],
  talk: 'text',
  head: 'h',
  barge: true,
  about: '',
  notes: true,
  delivery: 'narrate',
  effort: 'instant',
  efforts: ['instant', 'low', 'medium', 'high', 'max'],
};
const machine = 'm1' as MachineId;

const mount = async () => {
  render(<VoiceLive machine={machine} mode="chat" context={() => ''} />);
  await act(async () => {});
};

afterEach(cleanup);

describe('VoiceLive', () => {
  it('has no button while the voice mode is off', async () => {
    state.current = { ...base, subscription: false, route: '', ready: false };
    await mount();
    expect(screen.queryByRole('button', { name: 'next.chatgpt.voice.button' })).toBeNull();
  });

  // the machine was off the hub for a moment when the chat opened; the button must come once it is back.
  it('asks again after a failed read and shows the button', async () => {
    vi.useFakeTimers();
    try {
      state.current = base;
      state.failures = 1;
      await mount();
      expect(screen.queryByRole('button', { name: 'next.chatgpt.voice.button' })).toBeNull();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(voiceStateRetryDelay(0));
      });
      expect(screen.getByRole('button', { name: 'next.chatgpt.voice.button' })).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });

  it('stays without a Codex sign-in, says to sign in with ChatGPT and leads there', async () => {
    state.current = { ...base, signedIn: false, route: '', ready: false };
    await mount();
    await act(async () =>
      fireEvent.click(screen.getByRole('button', { name: 'next.chatgpt.voice.noLogin' })),
    );
    // Nothing grows over the screen: the reason and the way out come in a toast, the field stays as it is.
    expect(toasts.list.at(-1)).toEqual({
      text: 'next.chatgpt.voice.fail.noLogin',
      action: 'next.chatgpt.voice.signIn',
    });
    expect(document.querySelector('.hx-voice')).toBeNull();
  });
});

describe('spokenResult', () => {
  it('drops code and markup and keeps it short', () => {
    expect(
      spokenResult('## Готово\n- поменял **заголовок** в `Главная`\n```js\nx()\n```\n[доска](http://x)'),
    ).toBe('Готово поменял заголовок в Главная доска');
    expect(spokenResult('a'.repeat(2000)).length).toBe(1501);
  });
});

describe('addPiece', () => {
  it('finishes the person’s open line even when the voice already started answering', () => {
    let lines = addPiece([], 'user', 'Поменяй ', false);
    lines = addPiece(lines, 'assistant', 'Пон', false);
    lines = addPiece(lines, 'user', 'Поменяй заголовок', true);
    lines = addPiece(lines, 'assistant', 'ял.', false);
    lines = addPiece(lines, 'assistant', 'Понял.', true);
    expect(lines).toEqual([
      { role: 'user', text: 'Поменяй заголовок', done: true },
      { role: 'assistant', text: 'Понял.', done: true },
    ]);
  });
});

describe('a call that never connects', () => {
  const sdp = (lines: string[]) => ['v=0', ...lines].join('\r\n');
  it('names the candidates by kind only and tells a blocked network from a server that named nowhere', async () => {
    const { candidateKinds, silentFail, VOICE_FAIL } =
      await import('@/next/features/voice-live/use-voice-call');
    const offered = sdp([
      'a=candidate:1 1 udp 2130706431 10.0.0.5 50000 typ host',
      'a=candidate:2 1 UDP 2130706430 10.0.0.6 50001 typ host',
      'a=candidate:3 1 tcp 1671430143 203.0.113.9 443 typ srflx raddr 0.0.0.0 rport 0',
    ]);
    expect(candidateKinds(offered)).toBe('udp host ×2, tcp srflx');
    expect(candidateKinds(offered)).not.toContain('10.0.0');
    expect(silentFail(offered)).toBe(VOICE_FAIL.silent);
    expect(silentFail(sdp(['m=audio 9 UDP/TLS/RTP/SAVPF 111']))).toBe(VOICE_FAIL.noRoute);
    expect(silentFail(undefined)).toBe(VOICE_FAIL.noRoute);
  });
});
