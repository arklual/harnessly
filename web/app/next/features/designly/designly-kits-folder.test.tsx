// @vitest-environment jsdom
// The folder «UI-киты»: first among the library's folders, the library's own (no menu, no drop place), there
// before the kits are read, and the kits inside it.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import DesignlyHome from '@/next/features/designly/designly-home';
import type { IKitShelfCard } from '@/next/features/designly/kit/kit-api';
import type { ILibraryOrganizer } from '@/next/features/designly/library-organizer';
import { t } from '@/next/i18n';

const card: IKitShelfCard = {
  key: 'acme-ui',
  kit: 'acme-ui',
  version: '1.0.0',
  theme: null,
  title: 'Acme UI',
  kitTitle: '',
  cover: '',
  layouts: 4,
};

const organizer: ILibraryOrganizer = {
  folders: [{ id: 'f1', name: 'Работа', parent: '' }],
  onFolderSave: vi.fn(),
  onFolderDelete: vi.fn(),
  onMove: vi.fn(),
  onDeleteMany: vi.fn(),
  trash: [],
  loadTrash: vi.fn(),
  onRestore: vi.fn(),
  onPurge: vi.fn(),
};

const Home = ({ kits, onOpenKit = vi.fn() }: { kits: IKitShelfCard[] | null; onOpenKit?: () => void }) => (
  <DesignlyHome
    cards={null}
    error={null}
    now={0}
    onPin={vi.fn()}
    onOpen={vi.fn()}
    onRetry={vi.fn()}
    organizer={organizer}
    compose={null}
    kits={{ cards: kits, cover: () => '', onOpen: onOpenKit }}
  />
);

beforeAll(() => {
  const observer = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  vi.stubGlobal('ResizeObserver', observer);
  window.matchMedia ??= ((query: string) =>
    ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }) as unknown as MediaQueryList) as typeof window.matchMedia;
});
afterEach(cleanup);

const folderName = () => t('next.kit.card.section');

describe('the kits folder', () => {
  it('stands first among the folders while the kits are still read, with no menu and no drop place', () => {
    const { container } = render(<Home kits={null} />);
    const tiles = [...container.querySelectorAll('.hx-dl__folders > .hx-dl-folder')];
    expect(tiles.map((el) => el.textContent)).toEqual([folderName(), 'Работа']);
    expect(tiles[0]?.hasAttribute('data-drop-folder')).toBe(false);
    expect(tiles[0]?.querySelector('.hx-dl-folder__more')).toBeNull();
    expect(container.querySelector('.hx-kit-shelf')).toBeNull();
  });

  it('opens on the kits, and its crumb leads back to the library', () => {
    const onOpenKit = vi.fn();
    const { container } = render(<Home kits={[card]} onOpenKit={onOpenKit} />);
    fireEvent.click(screen.getByText(folderName()));
    expect(container.querySelector('.hx-dl__folders')).toBeNull();
    fireEvent.click(screen.getByText('Acme UI'));
    expect(onOpenKit).toHaveBeenCalledWith(card);
    expect(container.querySelector('.hx-dl__crumb[data-drop-folder]')).toBeNull();
    fireEvent.click(container.querySelector('.hx-dl__crumb') as HTMLButtonElement);
    expect(container.querySelector('.hx-dl__folders .hx-dl-folder')?.textContent).toBe(folderName());
  });

  it('shows the skeleton inside while the kits are read, and says when there are none', () => {
    const { container, rerender } = render(<Home kits={null} />);
    fireEvent.click(screen.getByText(folderName()));
    expect(container.querySelector('.hx-kit-shelf[aria-busy="true"]')).not.toBeNull();
    rerender(<Home kits={[]} />);
    expect(screen.getByText(t('next.kit.card.empty'))).toBeTruthy();
  });
});
