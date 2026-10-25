// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IDictationEvents } from '@/next/features/composer/voice-socket';

vi.mock('@/next/bindings/use-limits', () => ({ useAgentLimits: () => null }));
vi.mock('@/next/bindings/use-context-window', () => ({ useContextWindow: () => null }));
const dictations: IDictationEvents[] = [];
vi.mock('@/next/features/composer/voice-socket', () => ({
  openDictation: (_m: string, _lang: string, on: IDictationEvents) => {
    dictations.push(on);
    let ready = false;
    return {
      get ready() {
        return ready;
      },
      send: () => undefined,
      stop: () => {
        ready = true;
      },
      abort: () => undefined,
    };
  },
}));

import ChatDock from '@/next/bindings/chat-dock';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, useFakeTime } from '@/next/core/testing/setup';
import { t } from '@/next/i18n';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { draftText } from '@/next/stores/drafts';

const A = 'tab-k3Jx9QmZ' as ConvId;
const B = 'tab-s2Qwai' as ConvId;

/** The chat stage as the shell keys it: another chat is another dock. */
const Stage = ({ conv, machine }: { conv: ConvId; machine: MachineId }) => (
  <ChatDock key={conv} conv={conv} machine={machine} />
);

const field = () => screen.getByRole('textbox', { name: t('next.composer.label') }) as HTMLTextAreaElement;

class FakeRecorder {
  static isTypeSupported = () => true;
  state: 'inactive' | 'recording' = 'inactive';
  mimeType = 'audio/webm';
  ondataavailable: ((e: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  start() {
    this.state = 'recording';
  }
  stop() {
    this.state = 'inactive';
    this.onstop?.();
  }
}

let machine: MachineId;

beforeEach(async () => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  useFakeTime();
  const rt = new FakeRuntime();
  rt.welcome = { ...rt.welcome, features: [...new Set([...rt.welcome.features, 'voice' as const])] };
  ({ m: machine } = await bootFake({ rt }));
  dictations.length = 0;
});
afterEach(() => {
  cleanup();
  hub.stop();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('the draft of a chat', () => {
  it('is back in the field after going to another chat and returning, with the caret at its end', () => {
    const view = render(<Stage conv={A} machine={machine} />);
    fireEvent.change(field(), { target: { value: 'Проверь миграцию и' } });
    expect(field().value).toBe('Проверь миграцию и');

    view.rerender(<Stage conv={B} machine={machine} />);
    expect(field().value).toBe('');
    fireEvent.change(field(), { target: { value: 'другой чат' } });

    view.rerender(<Stage conv={A} machine={machine} />);
    expect(field().value).toBe('Проверь миграцию и');
    expect(field().selectionStart).toBe('Проверь миграцию и'.length);
    view.rerender(<Stage conv={B} machine={machine} />);
    expect(field().value).toBe('другой чат');
  });

  it('keeps what was dictated when the person leaves the chat mid-dictation, and the end of it lands in that chat', async () => {
    const stream = { getTracks: () => [{ stop: () => undefined }] };
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
      value: { getUserMedia: () => Promise.resolve(stream) },
    });
    const view = render(<Stage conv={A} machine={machine} />);
    fireEvent.change(field(), { target: { value: 'Начало.' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: t('next.composer.voice') }));
    });
    expect(dictations).toHaveLength(1);
    act(() => dictations[0].onPartial('проверь логи'));
    expect(field().value).toBe('Начало. проверь логи');

    // Off to another chat while speaking: before, the field went back to «Начало.».
    view.rerender(<Stage conv={B} machine={machine} />);
    expect(draftText(A)).toBe('Начало. проверь логи');
    act(() => dictations[0].onFinal('проверь логи деплоя'));
    expect(draftText(A)).toBe('Начало. проверь логи деплоя');
    expect(draftText(B)).toBe('');

    view.rerender(<Stage conv={A} machine={machine} />);
    expect(field().value).toBe('Начало. проверь логи деплоя');
  });
});
