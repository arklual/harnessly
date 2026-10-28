// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import useTimeline from '@/hooks/use-timeline';
import { setViewMachine } from '@/lib/app-path';

const sockets = vi.hoisted(() => ({
  made: [] as Array<{
    options: {
      url: () => string;
      onMessage: (data: unknown) => void;
    };
    sent: string[];
    closed: boolean;
  }>,
}));

vi.mock('@/runtime/socket', () => ({
  ReconnectingSocket: class {
    private readonly own;

    constructor(options: { url: () => string; onMessage: (data: unknown) => void }) {
      this.own = { options, sent: [] as string[], closed: false };
      sockets.made.push(this.own);
    }

    send(data: string) {
      this.own.sent.push(data);
      return true;
    }

    close() {
      this.own.closed = true;
    }
  },
}));

const props = (machineId: string) => ({
  sessionName: 'pt-ws-pane-tab',
  agentSessionId: '01a179e8-1111-4111-8111-111111111111',
  panelType: 'codex-cli' as const,
  machineId,
});

describe('a legacy timeline opened from the global conversation list', () => {
  beforeEach(() => {
    sockets.made = [];
    document.head.innerHTML = '<meta name="api-base" content="/agents">';
    window.history.replaceState(null, '', '/agents/');
    setViewMachine('machine-on-screen');
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ entries: [], startByteOffset: 0, hasMore: false }), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
      ),
    );
  });

  afterEach(() => {
    cleanup();
    setViewMachine(undefined);
    vi.unstubAllGlobals();
  });

  it('keeps the live socket, resume and older pages on the conversation origin machine', async () => {
    const view = renderHook(() => useTimeline(props('origin-machine')));
    const socket = sockets.made[0]!;
    expect(new URL(socket.options.url()).searchParams.get('machine')).toBe('origin-machine');

    act(() => {
      socket.options.onMessage(
        JSON.stringify({
          type: 'timeline:init',
          entries: [],
          sessionId: props('origin-machine').agentSessionId,
          jsonlPath: '/home/coder/.codex/sessions/rollout.jsonl',
          hasMore: true,
          startByteOffset: 1024,
        }),
      );
    });

    act(() => expect(view.result.current.resumeSession()).toBe(true));
    expect(JSON.parse(socket.sent.at(-1)!)).toMatchObject({
      type: 'timeline:resume',
      sessionId: props('origin-machine').agentSessionId,
    });

    await act(async () => view.result.current.loadMore());
    const request = String(vi.mocked(fetch).mock.calls.at(-1)![0]);
    expect(new URL(request, 'http://localhost').searchParams.get('machine')).toBe('origin-machine');
  });

  it('reopens the socket on the new origin when the shown conversation changes machines', () => {
    const view = renderHook(({ machineId }) => useTimeline(props(machineId)), {
      initialProps: { machineId: 'machine-a' },
    });
    const first = sockets.made[0]!;

    view.rerender({ machineId: 'machine-b' });

    expect(first.closed).toBe(true);
    expect(sockets.made).toHaveLength(2);
    expect(new URL(sockets.made[1]!.options.url()).searchParams.get('machine')).toBe('machine-b');
  });
});
