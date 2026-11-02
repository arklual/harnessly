// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import DesignlyHome, { type IDesignCard } from '@/next/features/designly/designly-home';
import type { IAppKind } from '@/next/features/app-catalog/api';

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
}: {
  onAppKind?: (k: IAppKind, brief: string) => Promise<void>;
  kinds?: IAppKind[] | null;
  cards?: IDesignCard[];
  onOpen?: (id: string) => void;
}) => (
  <DesignlyHome
    cards={cards}
    error={null}
    now={0}
    onPin={vi.fn()}
    onOpen={onOpen}
    onRetry={vi.fn()}
    compose={{
      systems: [],
      onCreate: vi.fn(),
      brief: '',
      onBrief: vi.fn(),
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
