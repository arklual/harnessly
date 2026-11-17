// @vitest-environment jsdom
// GitHub #108: the folder of the latest chat is gone; the machine refuses a chat there, the words stay in the field,
// and the next send starts the chat in the machine's default folder.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

vi.mock('@/next/bindings/use-limits', () => ({ useAgentLimits: () => null }));
vi.mock('@/next/bindings/use-context-window', () => ({ useContextWindow: () => null }));
const toasts: string[] = [];
vi.mock('@/next/ui/toast', async (load) => ({
  ...(await load<typeof import('@/next/ui/toast')>()),
  toast: (text: string) => {
    toasts.push(text);
  },
}));

import HomeComposer from '@/next/bindings/home-composer';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, useFakeTime } from '@/next/core/testing/setup';
import { t } from '@/next/i18n';
import type { Command, MachineId } from '@/next/protocol/v2';

let rt: FakeRuntime;
let machine: MachineId;

const field = () => screen.getByRole('textbox', { name: t('next.composer.label') }) as HTMLTextAreaElement;

beforeEach(async () => {
  useFakeTime();
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
      disconnect() {}
    },
  );
  rt = new FakeRuntime();
  toasts.length = 0;
  ({ m: machine } = await bootFake({ rt }));
});
afterEach(() => {
  cleanup();
  hub.stop();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it('a gone folder is refused, the words stay, and the next send goes to the default folder', async () => {
  rt.handlers.set('conversation.create', (cmd, r) =>
    (cmd as { body: { cwd?: string } }).body.cwd === '/gone'
      ? { r: 'refused', code: 'not-found' }
      : r.defaultCreate(cmd as Extract<Command, { op: 'conversation.create' }>),
  );
  const conversations = [{ ...rt.conversations[0], cwd: '/gone' }];
  render(<HomeComposer machine={machine} conversations={conversations} onCreated={() => undefined} />);
  fireEvent.change(field(), { target: { value: 'сделай отчёт' } });
  fireEvent.keyDown(field(), { key: 'Enter' });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(50);
  });
  expect(field().value).toBe('сделай отчёт');
  expect(toasts).toEqual([t('next.newChat.folderGone', { folder: '/gone' })]);
  fireEvent.keyDown(field(), { key: 'Enter' });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(50);
  });
  const creates = rt.commands.filter((c) => c.op === 'conversation.create') as { body: { cwd?: string } }[];
  expect(creates.map((c) => c.body.cwd)).toEqual(['/gone', '/home/fake']);
});
