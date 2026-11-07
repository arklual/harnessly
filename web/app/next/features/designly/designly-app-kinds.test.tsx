// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createRef } from 'react';
import { PortalContext } from '@/next/ui/portal';
import DesignlyHome, { type IDesignCard } from '@/next/features/designly/designly-home';
import type { IAppKind } from '@/next/features/app-catalog/api';
import type { MachineId } from '@/next/protocol/v2';

const kind: IAppKind = {
  id: 'acme.moodboard',
  title: 'Мудборд',
  app: 'acme.moodboard',
  appName: 'Мудборд команды',
  schemaVersion: 2,
};

const doc: IDesignCard = {
  id: 'idei-1',
  title: 'Идеи для лендинга',
  boards: 0,
  updatedAt: 0,
  image: null,
  kind: 'site',
  pinned: false,
  drawing: false,
  owner: null,
  canDelete: false,
  appKind: 'Мудборд',
};

const Home = ({
  onAppKind,
  kinds = [kind],
  cards = [],
  onOpen = vi.fn(),
  onDelete,
}: {
  onAppKind?: (k: IAppKind, brief: string) => Promise<void>;
  kinds?: IAppKind[] | null;
  cards?: IDesignCard[];
  onOpen?: (id: string) => void;
  onDelete?: (id: string) => Promise<void>;
}) => (
  <DesignlyHome
    cards={cards}
    error={null}
    now={0}
    onPin={vi.fn()}
    onOpen={onOpen}
    onDelete={onDelete}
    onRetry={vi.fn()}
    compose={{
      machine: 'm1' as MachineId,
      systems: [],
      onCreate: vi.fn(),
      brief: '',
      onBrief: vi.fn(),
      uploads: {
        attachments: [],
        ready: [],
        uploading: false,
        onFiles: vi.fn(),
        onRemove: vi.fn(),
        onRetry: vi.fn(),
        reset: vi.fn(),
      },
      picker: null,
      voice: { recording: null, onCancel: vi.fn(), onFinish: vi.fn() },
      appKinds: kinds,
      onAppKind,
    }}
  />
);

// jsdom has no layout observers; the row of tiles only needs them to exist.
beforeAll(() => {
  const observer = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  vi.stubGlobal('ResizeObserver', observer);
  // The settings panel of the start asks whether the window is a phone's.
  window.matchMedia ??= ((query: string) =>
    ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }) as unknown as MediaQueryList) as typeof window.matchMedia;
});
afterEach(cleanup);

describe('the tiles of the types apps add', () => {
  it('offers a tile for a type and waits for the machine before it is free again', async () => {
    let done: () => void = () => undefined;
    const onAppKind = vi.fn(() => new Promise<void>((resolve) => (done = resolve)));
    render(<Home onAppKind={onAppKind} />);
    const tile = screen.getByText('Мудборд').closest('button') as HTMLButtonElement;
    expect(screen.getByText('Мудборд команды')).toBeTruthy();
    fireEvent.click(tile);
    expect(onAppKind).toHaveBeenCalledWith(kind, '');
    // One at a time: the tile is busy and a second click does nothing.
    expect(tile.disabled).toBe(true);
    fireEvent.click(tile);
    expect(onAppKind).toHaveBeenCalledTimes(1);
    done();
    await waitFor(() => expect(tile.disabled).toBe(false));
  });

  it('frees the tile again when the machine refuses', async () => {
    const onAppKind = vi.fn(() => Promise.reject(new Error('no')));
    render(<Home onAppKind={onAppKind} />);
    const tile = screen.getByText('Мудборд').closest('button') as HTMLButtonElement;
    fireEvent.click(tile);
    await waitFor(() => expect(tile.disabled).toBe(false));
  });

  it('shows no tile while the types load or when there is no handler', () => {
    render(<Home kinds={null} onAppKind={vi.fn()} />);
    expect(screen.queryByText('Мудборд')).toBeNull();
    cleanup();
    render(<Home />);
    expect(screen.queryByText('Мудборд')).toBeNull();
  });

  it('lists a document of an app type with its title and type, opens it, and offers no pin or delete', () => {
    const onOpen = vi.fn();
    render(<Home cards={[doc]} kinds={null} onOpen={onOpen} />);
    const card = screen.getByRole('button', { name: 'Идеи для лендинга' });
    expect(card.textContent).toContain('Мудборд');
    // The line under the title is the type and a date; no counters of boards, no technical badges.
    expect(card.textContent).not.toMatch(/доск|board/i);
    expect(card.querySelector('.hx-dl-card__pin')).toBeNull();
    expect(card.querySelector('.hx-dl-card__del')).toBeNull();
    fireEvent.click(card);
    expect(onOpen).toHaveBeenCalledWith('idei-1');
  });
});

describe('the «…» menu of a card (HRN-872)', () => {
  it('«Выбрать» starts the selection with the card and does not open it, though the menu sits in a portal', async () => {
    const onOpen = vi.fn();
    const mine: IDesignCard = {
      id: 'mine-1',
      title: 'Мой макет',
      boards: 1,
      updatedAt: 0,
      image: null,
      kind: 'site',
      pinned: false,
      drawing: false,
      owner: null,
      canDelete: true,
    };
    // Menus mount in the root's popup host, as in the app.
    const host = createRef<HTMLDivElement>();
    render(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <Home cards={[mine]} kinds={null} onOpen={onOpen} onDelete={vi.fn(() => Promise.resolve())} />
      </PortalContext.Provider>,
    );
    const card = screen.getByRole('button', { name: 'Мой макет' });
    fireEvent.click(card.querySelector('.hx-dl-card__del') as HTMLElement);
    const items = await waitFor(() => {
      const found = document.querySelectorAll<HTMLElement>('.hx-cmenu__item');
      expect(found.length).toBeGreaterThan(0);
      return found;
    });
    // Without folders the first item is «Выбрать», the last «Удалить».
    fireEvent.click(items[0] as HTMLElement);
    expect(onOpen).not.toHaveBeenCalled();
    await waitFor(() => expect(card.getAttribute('data-selected')).toBe('true'));
  });
});
