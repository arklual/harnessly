// @vitest-environment jsdom
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import { setViewMachine } from '@/lib/app-path';
import { collectPeerMessages, collectViewableFiles, collectWorkPaths } from '@/lib/hl-artifacts';
import type { ITab } from '@/types/terminal';
import { paneEntries, paneLinks } from '@/next/legacy/pane-feed';
import { PaneSource } from '@/next/legacy/pane-source';
import { setScheduler } from '@/next/core/store';
import { applyPane, paneStore, setPane } from '@/next/stores/conversation';
import type { Conversation, Pane } from '@/next/protocol/v2';

const sockets = vi.hoisted(() => ({
  made: [] as Array<{ options: { url: () => string }; closed: boolean }>,
}));

vi.mock('@/runtime/socket', () => ({
  ReconnectingSocket: class {
    private readonly own;

    constructor(options: { url: () => string }) {
      this.own = { options, closed: false };
      sockets.made.push(this.own);
    }

    send() {
      return true;
    }

    close() {
      this.own.closed = true;
    }
  },
}));

const CONV = 'tab-one';
const SESSION = 'hs-ws-pane-tab-one';
const conv = { id: CONV, provider: 'claude', machineId: 'm1' } as unknown as Conversation;
const tab: ITab = {
  id: 'tab-one',
  sessionName: SESSION,
  name: 'work',
  order: 0,
  cwd: '/w',
  panelType: 'claude-code',
  agentState: {
    providerId: 'claude',
    sessionId: 's-1',
    jsonlPath: '/h/.claude/projects/w/s-1.jsonl',
    summary: null,
  },
};

const pane: Pane = {
  count: 40,
  links: [
    { url: 'https://docs.example.com/a/report-x1', label: 'The report', at: 30, count: 2, kind: 'artifact' },
  ],
  items: [
    {
      id: 't1',
      k: 'tool',
      at: 10,
      tool: 'Write',
      use: 'w1',
      path: '/w/report.md',
      text: 'Create /w/report.md',
    },
    { id: 't2', k: 'tool', at: 20, tool: 'SendMessage', use: 's1', peer: { to: 'scout', text: 'go on' } },
  ],
};

const published = () => useTimelineSnapshotStore.getState().bySession[SESSION];

describe('what the right pane reads of a conversation', () => {
  beforeEach(() => {
    setScheduler('sync');
    sockets.made = [];
    paneStore.delete(CONV);
    useTimelineSnapshotStore.setState({ bySession: {} });
    document.head.innerHTML = '<meta name="api-base" content="/agents">';
    window.history.replaceState(null, '', '/agents/');
    setViewMachine('m1');
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
    vi.unstubAllGlobals();
  });

  it('opens no timeline socket when the runtime sends the pane in the conversation channel', () => {
    setPane(CONV, pane);
    render(<PaneSource tab={tab} conv={conv} />);

    expect(sockets.made).toHaveLength(0);
    const got = published();
    expect(got.jsonlPath).toBe('/h/.claude/projects/w/s-1.jsonl');
    expect(got.activity).toBe(40);
    expect(got.links).toEqual([
      {
        url: 'https://docs.example.com/a/report-x1',
        label: 'The report',
        timestamp: 30,
        count: 2,
        kind: 'artifact',
        group: 'artifact',
      },
    ]);
    expect(got.entries.map((entry) => entry.id)).toEqual(['t1', 't2']);
  });

  it('follows the conversation channel as the agent works, still with no socket', () => {
    setPane(CONV, pane);
    render(<PaneSource tab={tab} conv={conv} />);
    act(() => {
      applyPane(CONV, {
        count: 44,
        items: [{ id: 't3', k: 'tool', at: 35, tool: 'Bash', use: 'b1', text: 'cat /w/notes.md' }],
      });
    });

    expect(sockets.made).toHaveLength(0);
    expect(published().entries.map((entry) => entry.id)).toEqual(['t1', 't2', 't3']);
    expect(published().activity).toBe(44);
    expect(published().links).toHaveLength(1);

    act(() => {
      applyPane(CONV, { count: 2, reset: true, items: [{ id: 'n1', k: 'notice', at: 50 }] });
    });
    expect(published().entries.map((entry) => entry.id)).toEqual(['n1']);
    expect(published().links).toEqual([]);
  });

  it('waits for the conversation snapshot before choosing a way', () => {
    render(<PaneSource tab={tab} conv={conv} />);

    expect(sockets.made).toHaveLength(0);
    expect(published()).toBeUndefined();
  });

  it('reads the Legacy timeline only from a runtime that sends no pane', () => {
    setPane(CONV, undefined);
    render(<PaneSource tab={tab} conv={conv} />);

    expect(sockets.made).toHaveLength(1);
    expect(sockets.made[0].options.url()).toContain('/api/timeline?');
  });

  it('leaves the timeline socket once the runtime starts sending the pane', () => {
    setPane(CONV, undefined);
    render(<PaneSource tab={tab} conv={conv} />);
    act(() => setPane(CONV, pane));

    expect(sockets.made).toHaveLength(1);
    expect(sockets.made[0].closed).toBe(true);
    expect(published().activity).toBe(40);
  });
});

describe('the pane items as the entries the contents read', () => {
  it('gives the contents the files, the paths and the messages to other agents', () => {
    const entries = paneEntries([
      ...(pane.items ?? []),
      { id: 'p1', k: 'patch', at: 36, files: ['/w/src/a.ts'] },
      { id: 'x1', k: 'exec', at: 37, text: 'ls /w/out', cwd: '/w' },
      { id: 'z1', k: 'something-new', at: 38 },
    ]);

    expect(entries.map((entry) => entry.type)).toEqual([
      'tool-call',
      'tool-call',
      'patch-apply',
      'exec-command-stream',
    ]);
    expect(collectViewableFiles(entries)).toEqual([
      { path: '/w/report.md', kind: 'markdown', origin: 'created', timestamp: 10 },
    ]);
    expect(collectPeerMessages(entries)).toEqual([
      { id: 't2', direction: 'out', peer: 'scout', text: 'go on', timestamp: 20 },
    ]);
    expect(collectWorkPaths(entries, null)).toEqual(['/w/out', '/w', '/w/src/a.ts', '/w/report.md']);
  });

  it('keeps one entry per item, so what was found in it is not looked for again', () => {
    const items = pane.items ?? [];
    expect(paneEntries(items)[0]).toBe(paneEntries([...items])[0]);
  });

  it('names the group of a link as the pane does', () => {
    expect(
      paneLinks([{ url: 'https://x.example/y', label: null, at: 1, count: 1, kind: 'web' }])[0].group,
    ).toBe('other');
  });
});
