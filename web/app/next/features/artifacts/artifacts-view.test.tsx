// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import ArtifactsView, { type IArtifactsViewProps } from '@/next/features/artifacts/artifacts-view';
import { ARTIFACT_KIND, type IArtifactItem } from '@/next/features/artifacts/model';
import { resetMetaStore, setMetaFetch } from '@/next/features/artifacts/meta-store';
import { SKELETON_DELAY_MS } from '@/next/ui/skeleton';

beforeAll(() => pinLocale('en'));
beforeEach(() => localStorage.clear());
afterEach(() => {
  cleanup();
  setMetaFetch(null);
  resetMetaStore();
  vi.useRealTimers();
});

const doc: IArtifactItem = {
  url: 'https://docs.example.com/a/plan',
  kind: ARTIFACT_KIND.doc,
  key: null,
  label: null,
  fallback: 'Document',
  where: 'docs.example.com/a/plan',
  github: false,
  readable: true,
  at: 3,
};
const task: IArtifactItem = {
  url: 'https://tracker.example.com/issue/ABC-7',
  kind: ARTIFACT_KIND.task,
  key: 'ABC-7',
  label: null,
  fallback: 'ABC-7',
  where: 'tracker.example.com/issue/ABC-7',
  github: false,
  readable: true,
  at: 2,
};

const props = (patch: Partial<IArtifactsViewProps> = {}): IArtifactsViewProps => ({
  machine: 'm1',
  convKey: 'c1',
  items: [doc, task],
  complete: true,
  failed: false,
  onReload: vi.fn(),
  onOpen: vi.fn(),
  ...patch,
});

const answers = (delayed: Record<string, Promise<void>> = {}) =>
  setMetaFetch(async (url) => {
    const asked = decodeURIComponent(url.split('url=')[1].split('&')[0]);
    await delayed[asked];
    if (asked === doc.url)
      return new Response(
        JSON.stringify({ kind: 'artifact', title: 'Release plan', description: 'What goes out' }),
      );
    return new Response(
      JSON.stringify({
        kind: 'issue',
        key: 'ABC-7',
        title: 'Fix the cart',
        state: 'In Progress',
        resolved: false,
        assignee: { name: 'Ann Lee', avatar: null },
      }),
    );
  });

describe('ArtifactsView', () => {
  it('stands the rows at once and fills the titles as they come, a bar in the title’s place meanwhile', async () => {
    let release: () => void = () => undefined;
    const held = new Promise<void>((resolve) => (release = resolve));
    answers({ [task.url]: held });
    render(<ArtifactsView {...props()} />);
    expect(screen.getByText('Documents')).toBeTruthy();
    expect(screen.getByText('Tasks')).toBeTruthy();
    expect(await screen.findByText('Release plan')).toBeTruthy();
    // the task is still being read: its key stands, its title is a bar, not a made-up word
    const row = screen.getByText('ABC-7').closest('a') as HTMLElement;
    expect(row.getAttribute('aria-busy')).toBe('true');
    expect(row.querySelector('.hx-sk')).not.toBeNull();
    expect(screen.queryByText('Document')).toBeNull();
    await act(async () => release());
    expect(await screen.findByText('Fix the cart')).toBeTruthy();
    expect(screen.getByText('ABC-7 · In Progress')).toBeTruthy();
    expect(row.getAttribute('aria-busy')).toBeNull();
  });

  it('has no counters in its headers', async () => {
    answers();
    render(<ArtifactsView {...props()} />);
    await screen.findByText('Release plan');
    for (const label of document.querySelectorAll('.hx-tv__label'))
      expect(label.textContent).not.toMatch(/\d/);
  });

  it('shows a skeleton while the links are not known, then the list in its place', async () => {
    vi.useFakeTimers();
    answers();
    const { container, rerender } = render(<ArtifactsView {...props({ items: [], complete: false })} />);
    act(() => void vi.advanceTimersByTime(SKELETON_DELAY_MS + 1));
    expect(container.querySelector('.hx-swap__sk .hx-art-sk')).not.toBeNull();
    expect(container.querySelector('.hx-art')).toBeNull();
    rerender(<ArtifactsView {...props()} />);
    expect(container.querySelector('.hx-swap')?.getAttribute('data-phase')).toBe('swapping');
    expect(container.querySelectorAll('.hx-art').length).toBe(2);
  });

  it('says there is nothing yet when the whole transcript is read and holds no links', () => {
    render(<ArtifactsView {...props({ items: [] })} />);
    expect(screen.getByText(/will appear here/)).toBeTruthy();
  });

  it('says it did not load, with «Повторить», when the runtime did not answer and nothing is known', () => {
    const onReload = vi.fn();
    render(<ArtifactsView {...props({ items: [], complete: false, failed: true, onReload })} />);
    expect(screen.getByText('Artifacts did not load')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onReload).toHaveBeenCalled();
  });

  it('keeps the rows when only the titles did not come, and asks them again on «Повторить»', async () => {
    let calls = 0;
    setMetaFetch(async () => {
      calls += 1;
      return calls <= 2
        ? new Response('', { status: 502 })
        : new Response(JSON.stringify({ kind: 'artifact', title: 'Release plan', description: null }));
    });
    vi.useFakeTimers();
    render(<ArtifactsView {...props({ items: [doc] })} />);
    await act(async () => void (await vi.advanceTimersByTimeAsync(5_000)));
    // the row stands with the word for its kind, and the list says some titles did not come
    expect(screen.getByText('Document')).toBeTruthy();
    expect(screen.getByText('Some links did not give their titles')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await act(async () => void (await vi.advanceTimersByTimeAsync(50)));
    expect(screen.getByText('Release plan')).toBeTruthy();
    expect(screen.queryByText('Some links did not give their titles')).toBeNull();
  });

  it('hands a click on a row to the bridge with its title', async () => {
    answers();
    const onOpen = vi.fn((_item, _title, event: { preventDefault: () => void }) => event.preventDefault());
    render(<ArtifactsView {...props({ onOpen })} />);
    fireEvent.click(await screen.findByText('Release plan'));
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onOpen.mock.calls[0][0]).toBe(doc);
    expect(onOpen.mock.calls[0][1]).toBe('Release plan');
  });

  it('draws what was seen in the conversation last time while the runtime reads it again', async () => {
    answers();
    const first = render(<ArtifactsView {...props()} />);
    await screen.findByText('Release plan');
    first.unmount();
    render(<ArtifactsView {...props({ items: [], complete: false })} />);
    expect(screen.getByText('Release plan')).toBeTruthy();
    await waitFor(() => expect(document.querySelectorAll('.hx-art').length).toBe(2));
  });
});
