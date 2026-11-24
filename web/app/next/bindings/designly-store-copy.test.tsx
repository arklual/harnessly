// @vitest-environment jsdom
// the machine answers the library with 504; the home shows the hub's copy from its store with a quiet line,
// not «Could not load the library».
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
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
        accessSource: 'link',
        role: 'view',
        accessUntil: '2030-11-05T22:00:00Z',
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
import { locale, t } from '@/next/i18n';

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
    const onOpen = vi.fn();
    render(<DesignlyPane machine={'m1' as MachineId} conv={'c1' as ConvId} design={null} onOpen={onOpen} />);
    expect(await screen.findByText('Лендинг из хранилища')).toBeTruthy();
    const when = new Intl.DateTimeFormat(locale(), { dateStyle: 'medium', timeStyle: 'short' }).format(
      new Date('2030-11-05T22:00:00Z'),
    );
    expect(
      screen.getByText(
        [
          t('next.designly.access.link'),
          t('next.access.role.view'),
          t('next.designly.access.until', { when }),
        ].join(' · '),
      ),
    ).toBeTruthy();
    expect(screen.getByRole('status').textContent).toBe(t('next.designly.storeCopy'));
    expect(screen.queryByText(t('next.designly.failed'))).toBeNull();
    fireEvent.click(screen.getByText('Лендинг из хранилища'));
    expect(onOpen).toHaveBeenCalledWith('site-1');
  });
});
