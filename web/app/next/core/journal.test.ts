// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  cleanUrl,
  flushJournal,
  journal,
  JOURNAL_QUEUE,
  newTrace,
  setJournalSender,
  startJournal,
  TRACE_HEADER,
  followTrace,
  type IJournalEntry,
} from '@/next/core/journal';
import type { MachineId } from '@/next/protocol/v2';

const m = 'local' as MachineId;

describe('the page journal (HRN-976)', () => {
  afterEach(() => {
    flushJournal();
    setJournalSender(null);
    vi.useRealTimers();
  });

  it('keeps an address without its query or a token in the path', () => {
    expect(cleanUrl('/agents/api/platform/apps/session?token=abc#x')).toBe(
      '/agents/api/platform/apps/session',
    );
    expect(cleanUrl('/aBcDeFgHiJkLmNoPqRsT123/example.shop/__hl/bridge-shim.js')).toBe(
      '/…/example.shop/__hl/bridge-shim.js',
    );
    expect(cleanUrl('https://slot.example/a/b.js?x=1')).toBe('https://slot.example/a/b.js');
    expect(cleanUrl('/RMSpaEcMVbGiK_zZSsyskZ-KcEKwlq/x.app/__hl/app-sdk.js')).toBe(
      '/…/x.app/__hl/app-sdk.js',
    );
    expect(
      cleanUrl('/__tk/s/eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2lnbmF0dXJlc2lnbmF0dXJl/v/ui/index.html'),
    ).toBe('/__tk/s/…/v/ui/index.html');
    expect(cleanUrl('/api/platform/conversation-history-sidebar')).toBe(
      '/api/platform/conversation-history-sidebar',
    );
  });

  it('gathers lines per machine, drops a repeat and what is past the queue', () => {
    vi.useFakeTimers();
    const sent: IJournalEntry[][] = [];
    setJournalSender((_m, entries) => {
      sent.push(entries);
      return Promise.resolve();
    });
    journal({ level: 'error', src: 'web', ev: 'resource', url: '/x.js' }, m);
    journal({ level: 'error', src: 'web', ev: 'resource', url: '/x.js' }, m);
    for (let i = 0; i < JOURNAL_QUEUE + 10; i++)
      journal({ level: 'info', src: 'web', ev: 'click', text: `b${i}` }, m);
    flushJournal();
    const all = sent.flat();
    expect(all.filter((e) => e.ev === 'resource')).toHaveLength(1);
    expect(all.length).toBe(JOURNAL_QUEUE);
    expect(all[0]?.at).toBeGreaterThan(0);
  });

  it('gives the requests of an action its trace and writes down a failed one', async () => {
    const sent: IJournalEntry[] = [];
    setJournalSender((_m, entries) => {
      sent.push(...entries);
      return Promise.resolve();
    });
    const seen: Headers[] = [];
    const fake = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      seen.push(new Headers(init?.headers));
      return Promise.resolve(new Response('{}', { status: 502 }));
    });
    const was = window.fetch;
    window.fetch = fake as unknown as typeof fetch;
    const stop = startJournal();
    try {
      const trace = newTrace('v-');
      followTrace(trace);
      await window.fetch('/api/platform/apps/session?machine=local', { method: 'POST' });
      expect(seen[0]?.get(TRACE_HEADER)).toBe(trace);
      flushJournal();
      const line = sent.find((e) => e.ev === 'fetch');
      expect(line).toMatchObject({
        level: 'error',
        method: 'POST',
        status: 502,
        trace,
        url: '/api/platform/apps/session',
      });
    } finally {
      stop();
      window.fetch = was;
    }
  });
});
