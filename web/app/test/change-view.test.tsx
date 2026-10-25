// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { afterEach, describe, expect, it, vi } from 'vitest';
import GitLabView from '@/components/agents/inspector/gitlab-view';
import type { IChangeView } from '@/lib/hl-gitlab-ref';

vi.mock('@/components/workspace/diff-file-list', () => ({
  default: ({ diff }: { diff: string }) => <pre data-testid="diff">{diff}</pre>,
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

dayjs.extend(relativeTime);

const URL_PR = 'https://github.com/ly-solutions/harnessly/pull/12';

const view = (over: Partial<IChangeView> = {}): IChangeView => ({
  kind: 'mr',
  provider: 'github',
  url: URL_PR,
  host: 'github.com',
  project: 'ly-solutions/harnessly',
  ref: '#12',
  title: 'Add the view',
  description: '## Why\n\nBecause **people** asked.',
  author: 'anna',
  at: '2026-10-20T10:00:00Z',
  state: 'opened',
  draft: false,
  sourceBranch: 'feat/view',
  targetBranch: 'main',
  pipeline: { status: 'failed', url: `${URL_PR}/checks` },
  diff: 'diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+b\ndiff --git a/b.txt b/b.txt\n--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-1\n+2',
  truncated: false,
  labels: ['bug'],
  stages: [
    {
      name: 'CI',
      status: 'failed',
      url: null,
      jobs: [
        {
          name: 'build',
          status: 'success',
          url: 'https://github.com/o/r/actions/runs/1/job/1',
          durationMs: 90000,
        },
        {
          name: 'test',
          status: 'failed',
          url: 'https://github.com/o/r/actions/runs/1/job/2',
          durationMs: null,
        },
      ],
    },
    {
      name: 'Lint',
      status: 'success',
      url: null,
      jobs: [{ name: 'eslint', status: 'success', url: null, durationMs: 4000 }],
    },
  ],
  comments: [
    {
      id: 'rc-1',
      resolved: false,
      path: 'a.txt',
      line: 1,
      outdated: false,
      notes: [
        { author: 'cy', at: '2026-10-20T08:00:00Z', body: 'rename this' },
        { author: 'anna', at: '2026-10-20T08:30:00Z', body: 'done' },
      ],
    },
    {
      id: 'rc-2',
      resolved: true,
      path: 'b.txt',
      line: 3,
      outdated: false,
      notes: [{ author: 'bo', at: '2026-10-20T09:00:00Z', body: 'fine by me' }],
    },
    {
      id: 'ic-1',
      resolved: null,
      path: null,
      line: null,
      outdated: false,
      notes: [{ author: 'dan', at: '2026-10-20T09:30:00Z', body: 'thanks' }],
    },
  ],
  limited: null,
  ...over,
});

const answer = (body: unknown, status = 200) => vi.fn(() => Promise.resolve(Response.json(body, { status })));

describe('the tab of a change', () => {
  it('shows a skeleton while the change loads', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => undefined)),
    );
    render(<GitLabView url={URL_PR} sessionName="s" tabId="t" />);
    expect(screen.getByRole('status')).toBeTruthy();
  });

  it('asks the change route for the link, whichever service it is on', async () => {
    const fetcher = answer(view());
    vi.stubGlobal('fetch', fetcher);
    render(<GitLabView url={URL_PR} sessionName="s" tabId="t" />);
    await screen.findByText('Add the view');
    const asked = String((fetcher.mock.calls[0] as unknown as [string])[0]);
    expect(asked).toContain('/api/platform/change-view?url=');
    expect(decodeURIComponent(asked)).toContain(URL_PR);
  });

  it('opens with the description, the checks and the discussion, and the diff folded', async () => {
    vi.stubGlobal('fetch', answer(view()));
    const { container } = render(<GitLabView url={URL_PR} sessionName="s" tabId="t" />);
    await screen.findByText('Add the view');

    // The description is read at once, as rendered Markdown.
    expect(screen.getByText('people').tagName).toBe('STRONG');
    expect(container.querySelector('details.tw-rpane-commit-description')).toBeNull();

    // The checks: a failed stage is open, a passed one is folded; a job opens its page.
    const stages = container.querySelectorAll<HTMLDetailsElement>('details.tw-change-stage');
    expect([...stages].map((s) => s.open)).toEqual([true, false]);
    const job = screen.getByRole('link', { name: /test/ });
    expect(job.getAttribute('href')).toBe('https://github.com/o/r/actions/runs/1/job/2');
    expect(job.getAttribute('target')).toBe('_blank');

    // The discussion: authors, the thread's place, resolved or not.
    expect(screen.getByText('rename this')).toBeTruthy();
    expect(screen.getByText('a.txt:1')).toBeTruthy();
    expect(screen.getByText('thanks')).toBeTruthy();
    const resolved = container.querySelector<HTMLDetailsElement>('.tw-change-thread[data-resolved] details');
    expect(resolved?.open).toBe(false);

    // The diff is a folded disclosure that counts the files and draws nothing until it is opened.
    const diff = container.querySelector<HTMLDetailsElement>('details.tw-change-diff');
    expect(diff?.open).toBe(false);
    expect(diff?.querySelector('summary')?.textContent).toMatch(/2/);
    expect(screen.queryByTestId('diff')).toBeNull();
    diff!.open = true;
    fireEvent(diff!, new Event('toggle'));
    expect(await screen.findByTestId('diff')).toBeTruthy();
  });

  it('puts the description and the pipeline of a commit on top, its diff folded', async () => {
    vi.stubGlobal(
      'fetch',
      answer(
        view({
          kind: 'commit',
          ref: 'c0ffee00',
          title: 'Fix the queue',
          description: 'It stalled when empty.',
          state: null,
          sourceBranch: null,
          targetBranch: null,
          comments: null,
        }),
      ),
    );
    const { container } = render(
      <GitLabView url="https://github.com/o/r/commit/c0ffee0" sessionName="s" tabId="t" />,
    );
    await screen.findByText('Fix the queue');
    expect(screen.getByText('It stalled when empty.')).toBeTruthy();
    expect(container.querySelector('.tw-change-stages')).not.toBeNull();
    expect(container.querySelector('.tw-change-threads')).toBeNull();
    expect(container.querySelector<HTMLDetailsElement>('details.tw-change-diff')?.open).toBe(false);
  });

  it('shows an issue without checks and a diff', async () => {
    vi.stubGlobal(
      'fetch',
      answer(
        view({
          kind: 'issue',
          ref: '#7',
          title: 'It crashes',
          diff: '',
          pipeline: null,
          stages: null,
          comments: [],
        }),
      ),
    );
    const { container } = render(
      <GitLabView url="https://github.com/o/r/issues/7" sessionName="s" tabId="t" />,
    );
    await screen.findByText('It crashes');
    expect(container.querySelector('details.tw-change-diff')).toBeNull();
    expect(container.querySelector('.tw-change-stages')).toBeNull();
  });

  it('is a card with a way out when the machine has no token', async () => {
    vi.stubGlobal(
      'fetch',
      answer(
        view({
          title: '',
          description: '',
          author: null,
          at: null,
          state: null,
          sourceBranch: null,
          targetBranch: null,
          pipeline: null,
          diff: '',
          labels: [],
          stages: null,
          comments: null,
          limited: 'no-token',
        }),
      ),
    );
    const { container } = render(<GitLabView url={URL_PR} sessionName="s" tabId="t" />);
    await waitFor(() => expect(container.querySelector('.tw-change-card')).not.toBeNull());
    const out = screen.getByRole('link', { name: /GitHub/ });
    expect(out.getAttribute('href')).toBe(URL_PR);
    expect(out.getAttribute('target')).toBe('_blank');
    expect(container.querySelector('iframe')).toBeNull();
    expect(screen.getByText('#12')).toBeTruthy();
  });

  it('keeps the way out when the route fails', async () => {
    vi.stubGlobal('fetch', answer({ error: 'nope' }, 502));
    render(<GitLabView url={URL_PR} sessionName="s" tabId="t" />);
    await screen.findByText('nope');
    expect(screen.getByRole('link', { name: /GitHub/ }).getAttribute('href')).toBe(URL_PR);
  });
});
