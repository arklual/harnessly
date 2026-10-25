// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import AppsView, { type IAppsViewProps } from '@/next/features/apps/apps-view';
import { DEFAULT_PREFS, setPrefs } from '@/next/features/apps/prefs';
import type { AppFeed, AppMR, AppRow, AppsFeed } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));

const phone = (on: boolean) => {
  window.matchMedia = ((query: string) => ({
    matches: on,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
};

beforeEach(() => {
  phone(false);
  Element.prototype.scrollTo = () => undefined;
  setPrefs(DEFAULT_PREFS);
});
afterEach(cleanup);

const NOW = 1_792_656_000_000;
const DAY = 86_400_000;

const row = (patch: Partial<AppRow>): AppRow => ({
  key: 'ABC-1',
  id: 'ABC-1',
  title: 'First',
  url: 'https://tracker.example.com/issue/ABC-1',
  state: 'In Progress',
  flag: null,
  tone: 'accent',
  project: null,
  person: 'Ann',
  target: null,
  createdAt: NOW - 20 * DAY,
  updatedAt: NOW - DAY,
  closedAt: null,
  author: null,
  assignees: [],
  reviewers: [],
  me: [],
  labels: [],
  priority: null,
  kind: null,
  parent: null,
  progress: null,
  children: [],
  mr: null,
  ...patch,
});

const epic = (patch: Partial<AppRow> = {}): AppRow =>
  row({
    kind: 'Epic',
    labels: ['repo:admin'],
    progress: { done: 1, total: 2 },
    children: [
      {
        id: 'ABC-11',
        title: 'Write the schema',
        url: 'https://tracker.example.com/issue/ABC-11',
        state: 'Done',
        done: true,
        person: 'Ann',
      },
      {
        id: 'ABC-12',
        title: 'Wire the screen',
        url: 'https://tracker.example.com/issue/ABC-12',
        state: 'In Progress',
        done: false,
        person: null,
      },
    ],
    ...patch,
  });

const mr = (patch: Partial<AppMR> = {}): AppMR => ({
  source: 'feature/login',
  draft: false,
  conflicts: false,
  needsRebase: false,
  ready: false,
  unresolved: false,
  comments: 0,
  pipeline: null,
  approvals: null,
  ...patch,
});

const merge = (id: string, patch: Partial<AppRow> = {}, extra: Partial<AppMR> = {}): AppRow =>
  row({
    key: id,
    id,
    title: `Merge ${id}`,
    url: `https://gitlab.example.com/api/-/merge_requests/${id.slice(1)}`,
    state: 'opened',
    project: 'api',
    target: 'main',
    author: 'Bob',
    mr: mr(extra),
    ...patch,
  });

const tracker = (epics: AppRow[], tasks: AppRow[] = []): AppFeed => ({
  id: 'tracker',
  state: 'ready',
  reason: null,
  host: 'tracker.example.com',
  me: 'ann',
  sections: [
    { id: 'epics', rows: epics },
    { id: 'tasks', rows: tasks },
  ],
});

const gitlab = (rows: AppRow[] = [], patch: Partial<AppFeed> = {}): AppFeed => ({
  id: 'gitlab',
  state: 'ready',
  reason: null,
  host: 'gitlab.example.com',
  me: 'ann',
  sections: [{ id: 'mrs', rows }],
  ...patch,
});

const feedOf = (...apps: AppFeed[]): AppsFeed => ({ at: NOW, staleDays: 5, apps });

const TASKS = [
  row({
    key: 'ABC-21',
    id: 'ABC-21',
    title: 'Fix the cart',
    kind: 'Bug',
    labels: ['repo:web'],
    state: 'New',
  }),
  row({
    key: 'OPS-3',
    id: 'OPS-3',
    title: 'Renew the certificate',
    state: 'Новая',
    labels: [],
    parent: { id: 'ABC-1', title: 'First', url: 'https://tracker.example.com/issue/ABC-1' },
  }),
];

const props = (patch: Partial<IAppsViewProps> = {}): IAppsViewProps => ({
  feed: feedOf(
    tracker(
      [
        epic(),
        epic({ key: 'ABC-2', id: 'ABC-2', title: 'Second', labels: [], children: [], progress: null }),
      ],
      TASKS,
    ),
    gitlab([
      merge('!1', { me: ['reviewer'], flag: 'review' }, { pipeline: 'success', ready: true }),
      merge(
        '!2',
        { me: ['author'], author: 'Ann', title: 'Stuck merge' },
        { pipeline: 'failed', conflicts: true, comments: 3 },
      ),
    ]),
  ),
  error: null,
  refreshing: false,
  app: 'tracker',
  onApp: vi.fn(),
  pageOpen: true,
  onBack: vi.fn(),
  picked: new Set(),
  onToggle: vi.fn(),
  onClear: vi.fn(),
  onChat: vi.fn(),
  onRetry: vi.fn(),
  onOpenDrawer: vi.fn(),
  onSettings: vi.fn(),
  now: NOW,
  ...patch,
});

describe('AppsView: the left list', () => {
  it('lists the connected apps on the left with the host, and marks the open one', () => {
    render(<AppsView {...props()} />);
    const nav = screen.getByRole('navigation', { name: 'Apps' });
    const items = within(nav).getAllByRole('button', { name: /Tracker|GitLab/ });
    expect(items.map((item) => item.textContent)).toEqual([
      'Trackertracker.example.com',
      'GitLabgitlab.example.com',
    ]);
    expect(items[0]!.getAttribute('aria-current')).toBe('true');
    expect(items[1]!.getAttribute('aria-current')).toBeNull();
  });

  it('says in the list what an app that cannot be read is, and opens another app on click', () => {
    const onApp = vi.fn();
    render(
      <AppsView
        {...props({
          onApp,
          feed: feedOf(tracker([]), gitlab([], { state: 'off', reason: 'no-token', sections: [] })),
        })}
      />,
    );
    const item = screen.getByRole('button', { name: /GitLab/ });
    expect(within(item).getByText('Not connected')).toBeTruthy();
    fireEvent.click(item);
    expect(onApp).toHaveBeenCalledWith('gitlab');
  });

  it('opens the drawer and the connected-apps settings, and goes back from the page', () => {
    const onOpenDrawer = vi.fn();
    const onSettings = vi.fn();
    const onBack = vi.fn();
    render(<AppsView {...props({ onOpenDrawer, onSettings, onBack })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Menu' }));
    fireEvent.click(screen.getByRole('button', { name: 'Connected apps' }));
    fireEvent.click(screen.getByRole('button', { name: 'All apps' }));
    expect(onOpenDrawer).toHaveBeenCalled();
    expect(onSettings).toHaveBeenCalled();
    expect(onBack).toHaveBeenCalled();
  });

  it('gives the page the phone`s slide through data-page', () => {
    const { container, rerender } = render(<AppsView {...props({ pageOpen: false })} />);
    expect(container.querySelector('.hx-hub')!.getAttribute('data-page')).toBe('false');
    rerender(<AppsView {...props({ pageOpen: true })} />);
    expect(container.querySelector('.hx-hub')!.getAttribute('data-page')).toBe('true');
  });

  it('stands a skeleton where the list and the page will be while the machine is asked', () => {
    vi.useFakeTimers();
    try {
      const { container } = render(<AppsView {...props({ feed: null })} />);
      act(() => {
        vi.advanceTimersByTime(200);
      });
      expect(container.querySelector('.hx-hubsk')).toBeTruthy();
      expect(container.querySelector('.hx-hub__sk')).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
    expect(screen.queryByRole('checkbox')).toBeNull();
    expect(screen.queryByRole('status')).toBeNull();
  });
});

describe('AppsView: the tracker', () => {
  it('draws the epics with their progress, the tag and the labels', () => {
    render(<AppsView {...props()} />);
    expect(screen.getByRole('heading', { name: 'Tracker' })).toBeTruthy();
    const first = screen.getAllByRole('checkbox')[0]!;
    expect(within(first).getByText('ABC-1')).toBeTruthy();
    expect(within(first).getByText('1 of 2')).toBeTruthy();
    expect(within(first).getByText('In progress')).toBeTruthy();
    expect(within(first).getByText('repo:admin')).toBeTruthy();
    expect(within(screen.getAllByRole('checkbox')[1]!).getByText('No tasks yet')).toBeTruthy();
  });

  it('opens the subtasks of an epic under it and folds them again', () => {
    render(<AppsView {...props()} />);
    expect(screen.queryByText('Write the schema')).toBeNull();
    const fold = screen.getAllByRole('button', { name: 'Subtasks' })[0]!;
    fireEvent.click(fold);
    expect(fold.getAttribute('aria-expanded')).toBe('true');
    const link = screen.getByText('Write the schema').closest('a')!;
    expect(link.getAttribute('href')).toBe('https://tracker.example.com/issue/ABC-11');
    expect(screen.getByText('Wire the screen')).toBeTruthy();
    fireEvent.click(fold);
    expect(fold.getAttribute('aria-expanded')).toBe('false');
  });

  it('opens the subtasks a search found, and only those', () => {
    render(<AppsView {...props()} />);
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search epics and tasks' }), {
      target: { value: 'schema' },
    });
    expect(screen.getAllByRole('checkbox')).toHaveLength(1);
    expect(screen.getByText('Write the schema')).toBeTruthy();
    expect(screen.queryByText('Wire the screen')).toBeNull();
  });

  it('shows the tasks on the person, ones from other spaces too, and groups them', () => {
    render(<AppsView {...props()} />);
    fireEvent.click(screen.getByRole('radio', { name: 'Tasks' }));
    const rows = screen.getAllByRole('checkbox');
    expect(rows.map((one) => one.querySelector('.hx-hubrow__id')!.textContent)).toEqual(['ABC-21', 'OPS-3']);
    expect(within(rows[1]!).getByText('ABC-1')).toBeTruthy();
    expect(within(rows[0]!).getByText('Bug')).toBeTruthy();
  });

  it('filters by a status and by a tag from the menus, and resets them', () => {
    phone(true);
    render(<AppsView {...props()} />);
    fireEvent.click(screen.getByRole('radio', { name: 'Tasks' }));
    fireEvent.click(screen.getByRole('button', { name: 'Tags' }));
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'repo:web' }));
    expect(screen.getAllByRole('checkbox', { name: /./ })).toBeTruthy();
    expect(screen.queryByText('Renew the certificate')).toBeNull();
    expect(screen.getByText('Fix the cart')).toBeTruthy();
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search epics and tasks' }), {
      target: { value: 'zzz' },
    });
    expect(screen.getByText('No row fits the search and the filters.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Reset the filters' }));
    expect(screen.getByText('Renew the certificate')).toBeTruthy();
  });

  it('says that there is nothing on the person instead of drawing nothing', () => {
    render(<AppsView {...props({ feed: feedOf(tracker([]), gitlab()) })} />);
    expect(screen.getByText('No epics on you')).toBeTruthy();
  });

  it('keeps finished work out until the page`s setting asks for it', () => {
    const done = row({ key: 'ABC-30', id: 'ABC-30', title: 'Shipped', state: 'Done', closedAt: NOW - DAY });
    const { rerender } = render(
      <AppsView {...props({ feed: feedOf(tracker([], [done, ...TASKS]), gitlab()) })} />,
    );
    fireEvent.click(screen.getByRole('radio', { name: 'Tasks' }));
    expect(screen.queryByText('Shipped')).toBeNull();
    act(() => setPrefs({ showDone: true }));
    rerender(<AppsView {...props({ feed: feedOf(tracker([], [done, ...TASKS]), gitlab()) })} />);
    expect(screen.getByText('Shipped')).toBeTruthy();
  });

  it('marks work that stands still with the days', () => {
    const old = row({
      key: 'ABC-40',
      id: 'ABC-40',
      title: 'Old',
      state: 'In Progress',
      updatedAt: NOW - 9 * DAY,
    });
    render(<AppsView {...props({ feed: feedOf(tracker([], [old]), gitlab()) })} />);
    fireEvent.click(screen.getByRole('radio', { name: 'Tasks' }));
    expect(screen.getByText('9 days without a move')).toBeTruthy();
  });
});

describe('AppsView: GitLab', () => {
  const open = (patch: Partial<IAppsViewProps> = {}) =>
    render(<AppsView {...props({ app: 'gitlab', ...patch })} />);

  it('draws a merge request with what is known of it', () => {
    open();
    expect(screen.getByRole('heading', { name: 'GitLab' })).toBeTruthy();
    const rows = screen.getAllByRole('checkbox');
    expect(rows).toHaveLength(2);
    const first = rows[0]!;
    expect(within(first).getByText('api')).toBeTruthy();
    expect(within(first).getByText('feature/login')).toBeTruthy();
    expect(within(first).getByText('Passed')).toBeTruthy();
    expect(within(first).getByText('Ready to merge')).toBeTruthy();
    expect(within(first).getByText('Review')).toBeTruthy();
    const second = rows[1]!;
    expect(within(second).getByText('Failed')).toBeTruthy();
    expect(within(second).getByText('Conflicts')).toBeTruthy();
    expect(within(second).getByText('3')).toBeTruthy();
  });

  it('narrows to what waits for the person, with the scope`s own buttons', () => {
    open();
    fireEvent.click(screen.getByRole('radio', { name: 'To review' }));
    expect(screen.getAllByRole('checkbox')).toHaveLength(1);
    expect(screen.getByText('Merge !1')).toBeTruthy();
    fireEvent.click(screen.getByRole('radio', { name: 'Mine' }));
    expect(screen.getByText('Stuck merge')).toBeTruthy();
    expect(screen.queryByText('Merge !1')).toBeNull();
  });

  it('searches by the title, the branch and the author', () => {
    open();
    const field = screen.getByRole('searchbox', { name: 'Search merge requests' });
    fireEvent.change(field, { target: { value: 'stuck' } });
    expect(screen.getAllByRole('checkbox')).toHaveLength(1);
    fireEvent.change(field, { target: { value: 'login bob' } });
    expect(screen.getAllByRole('checkbox')).toHaveLength(1);
    expect(screen.getByText('Merge !1')).toBeTruthy();
  });

  it('filters by the pipeline from a menu', () => {
    phone(true);
    open();
    fireEvent.click(screen.getByRole('button', { name: 'Pipeline' }));
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Failed' }));
    expect(screen.getAllByRole('checkbox')).toHaveLength(1);
    expect(screen.getByText('Stuck merge')).toBeTruthy();
  });

  it('opens the merged ones when the state is chosen', () => {
    phone(true);
    const merged = merge('!9', { state: 'merged', title: 'Done long ago', closedAt: NOW - DAY });
    open({ feed: feedOf(tracker([]), gitlab([merge('!1'), merged])) });
    expect(screen.queryByText('Done long ago')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Open' }));
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Merged' }));
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Open' }));
    expect(screen.getByText('Done long ago')).toBeTruthy();
    expect(screen.queryByText('Merge !1')).toBeNull();
  });
});

describe('AppsView: the phone', () => {
  it('opens a filter as a sheet', () => {
    phone(true);
    render(<AppsView {...props({ app: 'gitlab' })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Pipeline' }));
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Passed' }));
    expect(screen.getAllByRole('checkbox')).toHaveLength(1);
    expect(screen.getByText('Merge !1')).toBeTruthy();
  });
});

describe('AppsView: picking and the chat', () => {
  it('picks a row by a click or the keyboard, and the check follows the picked set', () => {
    const onToggle = vi.fn();
    const { rerender } = render(<AppsView {...props({ onToggle })} />);
    const [first, second] = screen.getAllByRole('checkbox');
    fireEvent.click(first!);
    fireEvent.keyDown(second!, { key: ' ' });
    expect(onToggle.mock.calls).toEqual([['ABC-1'], ['ABC-2']]);
    expect(first!.getAttribute('aria-checked')).toBe('false');
    rerender(<AppsView {...props({ onToggle, picked: new Set(['ABC-1']) })} />);
    expect(screen.getAllByRole('checkbox')[0]!.getAttribute('aria-checked')).toBe('true');
  });

  it('takes one row into a new chat from its own button without picking it', () => {
    const onChat = vi.fn();
    const onToggle = vi.fn();
    render(<AppsView {...props({ onChat, onToggle })} />);
    fireEvent.click(screen.getAllByRole('button', { name: 'Discuss in a new chat' })[1]!);
    expect(onChat).toHaveBeenCalledTimes(1);
    expect((onChat.mock.calls[0]![0] as AppRow[]).map((r) => r.id)).toEqual(['ABC-2']);
    expect(onToggle).not.toHaveBeenCalled();
  });

  it('takes a merge request into a chat from its own button', () => {
    const onChat = vi.fn();
    render(<AppsView {...props({ app: 'gitlab', onChat })} />);
    fireEvent.click(screen.getAllByRole('button', { name: 'Discuss in a new chat' })[0]!);
    expect((onChat.mock.calls[0]![0] as AppRow[]).map((r) => r.id)).toEqual(['!1']);
  });

  it('shows the bar for the picked rows, takes them into a chat, and lets the bar go with an exit', () => {
    vi.useFakeTimers();
    try {
      const onChat = vi.fn();
      const onClear = vi.fn();
      const open = props({ onChat, onClear, picked: new Set(['ABC-1', 'ABC-2']) });
      const { rerender } = render(<AppsView {...open} />);
      const bar = screen.getByRole('region', { name: 'Selected' });
      expect(within(bar).getByText('2 tasks')).toBeTruthy();
      fireEvent.click(within(bar).getByRole('button', { name: 'Clear selection' }));
      expect(onClear).toHaveBeenCalled();
      fireEvent.click(within(bar).getByRole('button', { name: 'Discuss in a new chat' }));
      expect((onChat.mock.calls[0]![0] as AppRow[]).map((r) => r.id)).toEqual(['ABC-1', 'ABC-2']);

      rerender(<AppsView {...props({ onChat, onClear })} />);
      const leaving = screen.getByRole('region', { name: 'Selected' });
      expect(leaving.getAttribute('data-state')).toBe('closing');
      expect(within(leaving).getByText('2 tasks')).toBeTruthy();
      act(() => {
        vi.advanceTimersByTime(300);
      });
      expect(screen.queryByRole('region', { name: 'Selected' })).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it('names a single picked row by its id and merge requests by their count', () => {
    const { rerender } = render(<AppsView {...props({ picked: new Set(['ABC-2']) })} />);
    expect(within(screen.getByRole('region', { name: 'Selected' })).getByText('ABC-2')).toBeTruthy();
    rerender(<AppsView {...props({ app: 'gitlab', picked: new Set(['!1', '!2']) })} />);
    expect(within(screen.getByRole('region', { name: 'Selected' })).getByText('2 MR')).toBeTruthy();
  });
});

describe('AppsView: what cannot be read', () => {
  it('says why an app is not there, with the host, and offers another look unless nothing can change', () => {
    const onRetry = vi.fn();
    const off = (patch: Partial<AppFeed>) => feedOf(tracker([]), gitlab([], { sections: [], ...patch }));
    const { rerender } = render(
      <AppsView {...props({ app: 'gitlab', onRetry, feed: off({ state: 'off', reason: 'no-token' }) })} />,
    );
    expect(screen.getByText('Not connected', { selector: '.hx-hubnote__title' })).toBeTruthy();
    expect(screen.getByText(/no token of yours for gitlab\.example\.com/)).toBeTruthy();
    expect(screen.getByText(/GITLAB_SELF_TOKEN/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalled();
    expect(screen.queryByRole('checkbox')).toBeNull();

    rerender(
      <AppsView {...props({ app: 'gitlab', feed: off({ state: 'failed', reason: 'unreachable' }) })} />,
    );
    expect(screen.getByText('Could not read')).toBeTruthy();
    expect(screen.getByText(/does not answer/)).toBeTruthy();

    rerender(
      <AppsView
        {...props({ app: 'gitlab', feed: off({ state: 'off', reason: 'not-configured', host: null }) })}
      />,
    );
    expect(screen.getByText(/no GitLab address/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
  });

  it('says that the machine could not be asked at all, and asks again on request', () => {
    const onRetry = vi.fn();
    render(<AppsView {...props({ feed: null, error: 'The machine did not answer in time', onRetry })} />);
    expect(screen.getByText('Could not load the apps')).toBeTruthy();
    expect(screen.getByText('The machine did not answer in time')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalled();
  });

  it('asks again from the refresh button of a page', () => {
    const onRetry = vi.fn();
    render(<AppsView {...props({ onRetry })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(onRetry).toHaveBeenCalled();
  });
});
