// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import ReviewPicker, { SEARCH_WAIT_MS } from '@/next/features/review/review-picker';
import type { MachineId } from '@/next/protocol/v2';

const MACHINE = 'm1' as MachineId;
const NOW = Date.parse('2026-11-11T00:00:00Z');

const answers: Record<string, unknown> = {
  projects: {
    state: 'ready',
    host: 'gitlab.example.com',
    items: [
      { path: 'team/app', name: 'app', url: 'https://gitlab.example.com/team/app', activity: null },
      { path: 'team/web', name: 'web', url: 'https://gitlab.example.com/team/web', activity: null },
    ],
  },
  mrs: {
    state: 'ready',
    items: [
      {
        iid: 5,
        title: 'Queue',
        url: 'https://gitlab.example.com/team/app/-/merge_requests/5',
        state: 'opened',
        draft: true,
        author: 'Fil',
        source: 'feat',
        target: 'main',
        updated: null,
      },
    ],
  },
};
let asked: URL[] = [];

beforeAll(() => pinLocale('en'));
beforeEach(() => {
  asked = [];
  localStorage.clear();
  vi.useFakeTimers({ shouldAdvanceTime: true });
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string) => {
      const url = new URL(input, 'http://localhost');
      asked.push(url);
      const body = answers[url.searchParams.get('list') ?? ''] ?? {
        state: 'off',
        reason: 'not-configured',
        items: [],
      };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const settle = async () => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(SEARCH_WAIT_MS + 200);
  });
};

describe('ReviewPicker', () => {
  it('walks a project to its merge request and opens it', async () => {
    const onOpen = vi.fn();
    render(<ReviewPicker machine={MACHINE} onOpen={onOpen} onSettings={() => undefined} now={NOW} />);
    await settle();
    fireEvent.click(await screen.findByText('app'));
    await settle();
    expect(asked.at(-1)?.searchParams.get('project')).toBe('team/app');
    expect(asked.at(-1)?.searchParams.get('filter')).toBe('open');
    expect(screen.getByText('Draft')).toBeTruthy();
    fireEvent.click(screen.getByText('Queue'));
    expect(onOpen).toHaveBeenCalledWith({
      link: expect.objectContaining({ kind: 'mr', project: 'team/app', ref: '5' }),
      title: 'app !5',
      name: 'Queue',
      source: 'feat',
      target: 'main',
    });
    // The project opened last stands on top next time.
    expect(localStorage.getItem('hx-review-recent')).toContain('team/app');
  });

  it('asks my reviews with the filter', async () => {
    render(<ReviewPicker machine={MACHINE} onOpen={vi.fn()} onSettings={() => undefined} now={NOW} />);
    await settle();
    fireEvent.click(await screen.findByText('web'));
    await settle();
    fireEvent.click(screen.getByRole('radio', { name: 'To review' }));
    await settle();
    expect(asked.at(-1)?.searchParams.get('filter')).toBe('review');
  });

  it('opens a pasted link without asking a list', async () => {
    const onOpen = vi.fn();
    render(<ReviewPicker machine={MACHINE} onOpen={onOpen} onSettings={() => undefined} now={NOW} />);
    await settle();
    const before = asked.length;
    fireEvent.change(screen.getByRole('searchbox'), {
      target: { value: 'https://gitlab.example.com/team/app/-/commit/0123456789' },
    });
    await settle();
    expect(asked.length).toBe(before);
    fireEvent.click(screen.getByText('Open app 01234567'));
    expect(onOpen).toHaveBeenCalledWith(
      expect.objectContaining({ link: expect.objectContaining({ kind: 'commit' }), title: 'app 01234567' }),
    );
  });

  it('says why there is no list', async () => {
    const kept = answers.projects;
    answers.projects = { state: 'off', reason: 'not-configured', items: [] };
    onTestFinished(() => {
      answers.projects = kept;
    });
    const onSettings = vi.fn();
    render(<ReviewPicker machine={MACHINE} onOpen={vi.fn()} onSettings={onSettings} now={NOW} />);
    await settle();
    expect(screen.getByText('GitLab is not connected to this installation.')).toBeTruthy();
    fireEvent.click(screen.getByText('Connections'));
    expect(onSettings).toHaveBeenCalled();
  });
});
