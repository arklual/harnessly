// @vitest-environment jsdom
// with none of the person's machines answering, the first message of a new chat is left with the hub, which
// makes the chat once a machine answers; the machine itself is not asked, and the field empties.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/next/bindings/use-limits', () => ({ useAgentLimits: () => null }));
vi.mock('@/next/bindings/use-context-window', () => ({ useContextWindow: () => null }));
const queued: { machine: string; req: unknown }[] = [];
vi.mock('@/next/commands/queued-chats', async (load) => ({
  ...(await load<typeof import('@/next/commands/queued-chats')>()),
  queueChat: (machine: string, req: unknown) => {
    queued.push({ machine, req });
    return Promise.resolve({
      ok: true,
      item: {
        id: 'q1',
        machineId: machine,
        clientId: 'c1',
        text: 'x',
        state: 'queued',
        error: null,
        convId: null,
        createdAt: 1,
      },
    });
  },
}));

import HomeComposer from '@/next/bindings/home-composer';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, useFakeTime } from '@/next/core/testing/setup';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { draftsStore, NEW_CHAT } from '@/next/stores/drafts';

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
  ({ m: machine } = await bootFake({ rt }));
  queued.length = 0;
});
afterEach(() => {
  cleanup();
  hub.stop();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('a new chat while no machine answers', () => {
  it('waits in the hub, the machine is not asked, the field empties and the waiting chat opens', async () => {
    const onQueued = vi.fn();
    const onCreated = vi.fn();
    render(
      <HomeComposer
        machine={machine}
        conversations={rt.conversations}
        onCreated={onCreated}
        queueWhenSilent
        onQueued={onQueued}
      />,
    );
    fireEvent.change(field(), { target: { value: 'сделай отчёт' } });
    fireEvent.keyDown(field(), { key: 'Enter' });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(queued).toHaveLength(1);
    expect(queued[0].machine).toBe(machine);
    expect(queued[0].req).toMatchObject({ first: { text: 'сделай отчёт' } });
    expect(rt.commands.filter((c) => c.op === 'conversation.create')).toHaveLength(0);
    expect(onQueued).toHaveBeenCalledWith('q1');
    expect(onCreated).not.toHaveBeenCalled();
    expect(draftsStore.has(NEW_CHAT)).toBe(false);
  });

  it('goes to the machine as before while one answers', async () => {
    const onQueued = vi.fn();
    render(
      <HomeComposer
        machine={machine}
        conversations={rt.conversations}
        onCreated={() => undefined}
        onQueued={onQueued}
      />,
    );
    fireEvent.change(field(), { target: { value: 'сделай отчёт' } });
    fireEvent.keyDown(field(), { key: 'Enter' });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(queued).toHaveLength(0);
    expect(onQueued).not.toHaveBeenCalled();
    expect(rt.commands.filter((c) => c.op === 'conversation.create')).toHaveLength(1);
  });
});
