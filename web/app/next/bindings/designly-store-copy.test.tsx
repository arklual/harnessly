// @vitest-environment jsdom
// the machine answers the library with 504; the home shows the hub's copy from its store with a quiet line,
// not «Could not load the library».
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { ConvId, MachineId } from '@/next/protocol/v2';

vi.mock('@/next/features/designly/api', async (orig) => ({
  ...(await orig<typeof import('@/next/features/designly/api')>()),
  fetchLibrary: vi.fn(async () => {
    throw new Error('504');
  }),
  fetchStoreLibrary: vi.fn(async () => ({
    designs: [
      {
        id: 'site-1',
        title: 'Лендинг из хранилища',
        boards: [{ title: 'Главная', path: 'home.dc.html' }],
        convs: [],
        kind: 'site',
        pinned: false,
        cover: null,
        projectPath: '',
        updatedAt: 1,
      },
    ],
    layout: { folders: [], placed: {}, systems: {} },
  })),
  fetchLayout: vi.fn(async () => {
    throw new Error('504');
  }),
  fetchEngine: vi.fn(async () => ({ ready: true })),
}));
vi.mock('@/next/features/app-catalog/api', async (orig) => ({
  ...(await orig<typeof import('@/next/features/app-catalog/api')>()),
  loadKindDocs: vi.fn(async () => []),
}));
vi.mock('@/next/stores/conversations', () => ({ useConversationList: () => [] }));
vi.mock('@/next/bindings/app-doc', () => ({
  useAppDocStage: (_m: string, id: string) => ({ title: null, stage: <div data-testid="stage">{id}</div> }),
}));
vi.mock('@/next/bindings/designly-site', () => ({
  DesignlyPaneSite: ({ id }: { id: string }) => <div data-testid="site">{id}</div>,
}));
import DesignlyPane from '@/next/bindings/designly-pane';
import { t } from '@/next/i18n';

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

describe('the library while the machine does not answer', () => {
  it('shows the hub copy with a quiet line instead of the error', async () => {
    render(<DesignlyPane machine={'m1' as MachineId} conv={'c1' as ConvId} design={null} onOpen={vi.fn()} />);
    expect(await screen.findByText('Лендинг из хранилища')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toBe(t('next.designly.storeCopy'));
    expect(screen.queryByText(t('next.designly.failed'))).toBeNull();
  });
});
