// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { ConvId, MachineId } from '@/next/protocol/v2';

vi.mock('@/next/features/designly/api', async (orig) => ({
  ...(await orig<typeof import('@/next/features/designly/api')>()),
  fetchLibrary: vi.fn(async () => ({
    port: 1,
    root: '/lib',
    designs: [
      {
        id: 'site-1',
        title: 'Лендинг',
        boards: [],
        convs: [],
        kind: 'site',
        pinned: false,
        updatedAt: 1,
      },
    ],
  })),
  fetchEngine: vi.fn(async () => ({ ready: true })),
}));
vi.mock('@/next/features/app-catalog/api', async (orig) => ({
  ...(await orig<typeof import('@/next/features/app-catalog/api')>()),
  loadKindDocs: vi.fn(async () => [
    {
      document: 'idei-1',
      title: 'Идеи для лендинга',
      updatedAt: 2,
      kind: 'acme.moodboard',
      kindTitle: 'Мудборд',
      needsApp: false,
    },
  ]),
}));
vi.mock('@/next/stores/conversations', () => ({ useConversationList: () => [] }));
vi.mock('@/next/bindings/app-doc', () => ({
  useAppDocStage: (_m: string, id: string) => ({ title: null, stage: <div data-testid="stage">{id}</div> }),
}));
vi.mock('@/next/bindings/designly-site', () => ({
  DesignlyPaneSite: ({ id }: { id: string }) => <div data-testid="site">{id}</div>,
}));
import DesignlyPane from '@/next/bindings/designly-pane';

const M = 'm1' as MachineId;
const CONV = 'c1' as ConvId;

beforeAll(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  );
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

describe('the library in the right pane', () => {
  it('lists the documents of an app type beside the designs, and a click opens that document', async () => {
    const onOpen = vi.fn();
    render(<DesignlyPane machine={M} conv={CONV} design={null} onOpen={onOpen} />);
    expect(await screen.findByText('Лендинг')).toBeTruthy();
    const card = await screen.findByText('Идеи для лендинга');
    fireEvent.click(card);
    expect(onOpen).toHaveBeenCalledWith('idei-1');
  });

  it('opens a document of an app type in its editor, a design in the viewer', async () => {
    const { unmount } = render(<DesignlyPane machine={M} conv={CONV} design="idei-1" onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByTestId('stage').textContent).toBe('idei-1'));
    expect(screen.queryByTestId('site')).toBeNull();
    unmount();
    render(<DesignlyPane machine={M} conv={CONV} design="site-1" onOpen={vi.fn()} />);
    await waitFor(() => expect(screen.getByTestId('site').textContent).toBe('site-1'));
    expect(screen.queryByTestId('stage')).toBeNull();
  });
});
