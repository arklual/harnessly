// @vitest-environment jsdom
// The caret stays where the person types in the chat's field: at the start of a draft, with a picture
// attached, with pauses between the keys long enough for the draft to go to the machine and come back.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/next/bindings/use-limits', () => ({ useAgentLimits: () => null }));
vi.mock('@/next/bindings/use-context-window', () => ({ useContextWindow: () => null }));

import ChatDock from '@/next/bindings/chat-dock';
import { MainFieldArea, OneFieldRoot } from '@/next/features/composer/one-field';
import { startDraftSync } from '@/next/core/drafts-cloud';
import { memoryKV, startPersistence, type Persistence } from '@/next/core/persist';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, useFakeTime } from '@/next/core/testing/setup';
import { t } from '@/next/i18n';
import type { CommandBodies, ConvId, Draft, MachineId } from '@/next/protocol/v2';
import { setDraft } from '@/next/stores/drafts';

const A = 'tab-k3Jx9QmZ' as ConvId;
const field = () => screen.getByRole('textbox', { name: t('next.composer.label') }) as HTMLTextAreaElement;
const setText = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;

let machine: MachineId;
let persistence: Persistence;
let stopSync: () => void;

beforeEach(async () => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  useFakeTime();
  const rt = new FakeRuntime();
  rt.welcome = { ...rt.welcome, features: [...new Set([...rt.welcome.features, 'drafts' as const])] };
  // The machine keeps the drafts and shows each write back on the convs channel, a round trip later.
  rt.latencyMs = 120;
  const book = new Map<string, Draft>();
  rt.handlers.set('draft.set', (cmd) => {
    const b = cmd.body as CommandBodies['draft.set'];
    if (b.rev !== (book.get(b.key)?.rev ?? 0) + 1) return { r: 'refused', code: 'conflict' };
    const d: Draft = { ...b, deviceId: 'dev-here', at: Date.now() };
    delete (d as Partial<CommandBodies['draft.set']>).opId;
    book.set(b.key, d);
    return {
      r: 'accepted',
      ch: 'convs',
      seq: rt.emit('convs', { type: 'draft_upsert', draft: d }),
      rev: b.rev,
    };
  });
  ({ m: machine } = await bootFake({ rt }));
  persistence = startPersistence(memoryKV());
  await persistence.ready;
  stopSync = startDraftSync();
});
afterEach(() => {
  cleanup();
  stopSync();
  persistence.stop();
  hub.stop();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const Window = () => (
  <>
    <MainFieldArea>
      <ChatDock conv={A} machine={machine} />
    </MainFieldArea>
    <OneFieldRoot />
  </>
);

/** A key as the browser types it: the text and the caret change, then the input event. */
const typeAtCaret = (el: HTMLTextAreaElement, key: string) => {
  const at = el.selectionStart;
  setText?.call(el, el.value.slice(0, at) + key + el.value.slice(el.selectionEnd));
  el.setSelectionRange(at + 1, at + 1);
  fireEvent.input(el);
};

describe('the caret in the chat’s field', () => {
  it.each([
    ['without a file', false],
    ['with a picture attached', true],
  ])('stays where the person types at the start of the text, %s', async (_, withFile) => {
    setDraft(A, {
      text: 'проверь логи',
      attachments: withFile ? [{ id: 'f1', name: 'screen.png', kind: 'image', mime: 'image/png' }] : [],
    });
    render(<Window />);
    await act(() => vi.advanceTimersByTimeAsync(0));
    const el = field();
    expect(el.value).toBe('проверь логи');
    el.focus();
    el.setSelectionRange(0, 0);
    let typed = '';
    for (const [key, pause] of [
      ['С', 0],
      ['р', 50],
      ['о', 400],
      ['ч', 800],
      ['н', 2500],
      ['о', 5000],
    ] as const) {
      typeAtCaret(el, key);
      typed += key;
      await act(() => vi.advanceTimersByTimeAsync(pause));
      expect(el.value).toBe(`${typed}проверь логи`);
      expect(el.selectionStart).toBe(typed.length);
    }
  });
});
