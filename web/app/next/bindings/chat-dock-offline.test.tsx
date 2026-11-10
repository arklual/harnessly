// @vitest-environment jsdom
// HRN-674: the machine restarts (an update) or is switched off while the person dictates or writes. The microphone is
// the browser's, so stop and cancel never wait for the machine; a message waits in the outbox and goes by itself.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IDictationEvents } from '@/next/features/composer/voice-socket';

// The dock with its runtime boots slowly on a busy machine.
vi.setConfig({ testTimeout: 30_000 });
vi.mock('@/next/bindings/use-limits', () => ({ useAgentLimits: () => null }));
vi.mock('@/next/bindings/use-context-window', () => ({ useContextWindow: () => null }));
const dictations: IDictationEvents[] = [];
const aborted: number[] = [];
vi.mock('@/next/features/composer/voice-socket', () => ({
  openDictation: (_m: string, _lang: string, _voice: unknown, on: IDictationEvents) => {
    const n = dictations.push(on) - 1;
    // A socket that is still on its way: it never says ready.
    return { ready: false, send: () => undefined, stop: () => undefined, abort: () => aborted.push(n) };
  },
}));

import ChatDock, { OFF_NOTICE_MS } from '@/next/bindings/chat-dock';
import { flushOutbox } from '@/next/commands';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { takesStore } from '@/next/features/composer/voice-takes';
import { t } from '@/next/i18n';
import type { Connection, ConvId, MachineId, Ms } from '@/next/protocol/v2';
import { centralStore } from '@/next/stores/central';
import { connectionStore } from '@/next/stores/connection';
import { messagesStore } from '@/next/stores/messages';
import { opsStore } from '@/next/stores/ops';

const CONV = 'tab-k3Jx9QmZ' as ConvId;

class FakeRecorder {
  static isTypeSupported = () => true;
  state: 'inactive' | 'recording' = 'inactive';
  mimeType = 'audio/webm';
  ondataavailable: ((e: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  start() {
    this.state = 'recording';
    this.ondataavailable?.({ data: new Blob(['речь'], { type: 'audio/webm' }) });
  }
  stop() {
    this.state = 'inactive';
    this.onstop?.();
  }
}

const field = () => screen.getByRole('textbox', { name: t('next.composer.label') }) as HTMLTextAreaElement;
const button = (name: string) => screen.getByRole('button', { name: new RegExp(`^${name}`) });
const away = (): Connection => ({
  k: 'offline',
  retryAt: (Date.now() + 5000) as Ms,
  cause: 'machine-unreachable',
});
/** The recording bar leaves with its closing animation. */
const barGone = async (name: string) => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(screen.queryByRole('button', { name: new RegExp(`^${name}`) })).toBeNull();
};
const failedTakes = () => [...takesStore.values()].filter((take) => take.state === 'failed');

let machine: MachineId;
let rt: FakeRuntime;

beforeEach(async () => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  vi.stubGlobal('MediaRecorder', FakeRecorder);
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia: () => Promise.resolve({ getTracks: () => [{ stop: () => undefined }] }) },
  });
  useFakeTime();
  rt = new FakeRuntime();
  rt.welcome = { ...rt.welcome, features: [...new Set([...rt.welcome.features, 'voice' as const])] };
  ({ m: machine } = await bootFake({ rt }));
  dictations.length = 0;
  aborted.length = 0;
  for (const id of [...takesStore.keys()]) takesStore.delete(id);
});
afterEach(() => {
  cleanup();
  hub.stop();
  centralStore.set({ enabled: false, off: new Set(), moved: new Set(), names: new Map(), on: new Set() });
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const startDictation = async () => {
  await act(async () => {
    fireEvent.click(button(t('next.composer.voice')));
  });
};

describe('a dictation while the machine is away', () => {
  it('stops at once without a link: no socket is tried, the recording is kept for later', async () => {
    render(<ChatDock conv={CONV} machine={machine} />);
    act(() => connectionStore.set(machine, away()));
    await startDictation();
    expect(dictations).toHaveLength(0);

    act(() => fireEvent.click(button(t('next.composer.finishRecording'))));
    // The bar is gone (the field is free again) and the recording waits over the field, calmly.
    expect(failedTakes()).toHaveLength(1);
    await barGone(t('next.composer.finishRecording'));
    expect(screen.getByText(t('next.dockBar.voiceWaiting'))).toBeTruthy();
    expect(screen.queryByRole('button', { name: t('next.dockBar.voiceRetry') })).toBeNull();
  });

  it('does not wait for a socket that will not come when the link drops before the stop', async () => {
    render(<ChatDock conv={CONV} machine={machine} />);
    await startDictation();
    expect(dictations).toHaveLength(1);
    // The machine restarts mid-sentence; its socket was still opening.
    act(() => connectionStore.set(machine, away()));
    act(() => fireEvent.click(button(t('next.composer.finishRecording'))));
    expect(aborted).toEqual([0]);
    expect(failedTakes()).toHaveLength(1);
    await barGone(t('next.composer.finishRecording'));
  });

  it('cancel drops the recording without the machine', async () => {
    render(<ChatDock conv={CONV} machine={machine} />);
    act(() => connectionStore.set(machine, away()));
    await startDictation();
    act(() => fireEvent.click(button(t('next.composer.cancelRecording'))));
    expect(failedTakes()).toHaveLength(0);
    await barGone(t('next.composer.cancelRecording'));
  });
});

describe('a message to a machine that is switched off', () => {
  it('waits in the outbox and goes by itself when the machine is back; the bar comes only after a while', async () => {
    act(() => {
      centralStore.set({
        enabled: true,
        off: new Set([machine]),
        moved: new Set(),
        names: new Map([[machine, 'dev-department']]),
        on: new Set([machine]),
      });
      connectionStore.set(machine, away());
    });
    rt.gatewayErrors = 99;
    render(<ChatDock conv={CONV} machine={machine} />);
    // The field is usable: before, it was quiet and nothing could be sent.
    expect(field().disabled).toBe(false);
    fireEvent.change(field(), { target: { value: 'Проверь выкат' } });
    await act(async () => {
      fireEvent.keyDown(field(), { key: 'Enter' });
      await settle();
    });
    const id = [...messagesStore.values()].find((m) => m.text === 'Проверь выкат')?.clientId ?? '';
    expect(messagesStore.get(id)).toMatchObject({
      text: 'Проверь выкат',
      state: { k: 'submitting' },
      refused: null,
    });
    expect(field().value).toBe('');
    // A restart for an update passes unseen; a machine away for long is said, calmly.
    expect(screen.queryByText(t('next.dockBar.offSub'))).toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(OFF_NOTICE_MS);
    });
    expect(screen.getByText(t('next.dockBar.offSub'))).toBeTruthy();

    // The machine is back: the message goes, once, with its clientId.
    rt.gatewayErrors = 0;
    await act(async () => {
      flushOutbox(machine);
      await vi.advanceTimersByTimeAsync(50);
    });
    // The runtime has it (its states then come on the chat's channel) and nothing sends it again.
    expect(opsStore.get(id)?.k).toBe('done');
    expect(messagesStore.get(id)).toMatchObject({ issue: null, refused: null });
    expect(new Set(rt.commands.filter((c) => c.op === 'send').map((c) => c.body.opId))).toEqual(
      new Set([id]),
    );
    const sends = () => rt.commands.filter((c) => c.op === 'send').length;
    const sent = sends();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(sends()).toBe(sent);
  });
});
