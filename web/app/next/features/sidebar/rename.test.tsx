// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createRef, useState } from 'react';
import { PortalContext } from '@/next/ui/portal';
import ChatRow, {
  RENAME,
  ROW_VARIANT,
  type IRowHandlers,
  type TRename,
} from '@/next/features/sidebar/chat-row';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';

afterEach(cleanup);

// jsdom has no matchMedia: a wide mouse window, not the phone.
beforeAll(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});

const host = createRef<HTMLDivElement>();
const chat: ISidebarChat = {
  id: 'tab-a',
  title: 'Old name',
  status: CHAT_STATUS.none,
  pinned: false,
  projectId: null,
  createdAt: 1,
  lastPromptAt: 1,
  lastActivityAt: 1,
  terminal: true,
  canStopTerminal: true,
};

const renameOf = (renaming: { saving: boolean } | null): TRename => {
  if (!renaming) return RENAME.off;
  return renaming.saving ? RENAME.saving : RENAME.editing;
};

/** The row as the sidebar drives it: the rename state is the sidebar's, the title is the chat's. */
const Harness = ({ onRename }: { onRename: (id: string, title: string) => Promise<void> }) => {
  const [renaming, setRenaming] = useState<{ saving: boolean } | null>(null);
  const [title, setTitle] = useState(chat.title);
  const handlers: IRowHandlers = {
    open: vi.fn(),
    stopTerminal: undefined,
    fork: undefined,
    renameStart: () => setRenaming({ saving: false }),
    renameEnd: (c, next) => {
      if (next === null) {
        setRenaming(null);
        return;
      }
      setRenaming({ saving: true });
      void onRename(c.id, next)
        .then(() => setTitle(next))
        .finally(() => setRenaming(null));
    },
    pin: vi.fn(),
    move: vi.fn(),
    remove: vi.fn(),
    place: vi.fn(),
    shift: vi.fn(),
    copyLink: undefined,
    openInNewTab: undefined,
    terminal: undefined,
  };
  return (
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      <ChatRow
        chat={{ ...chat, title }}
        variant={ROW_VARIANT.item}
        current={false}
        pending={undefined}
        fresh={false}
        rename={renameOf(renaming)}
        projects={[]}
        actions={{ onRename }}
        favGroups={[]}
        slot={null}
        handlers={handlers}
      />
    </PortalContext.Provider>
  );
};

const openRename = async () => {
  fireEvent.click(screen.getAllByRole('button').find((b) => b.className.includes('hx-item__menu'))!);
  fireEvent.click(await screen.findByRole('menuitem', { name: /rename|переименовать/i }));
};
const settle = () => act(() => new Promise<void>((r) => setTimeout(r, 400)));

describe('renaming a chat in the sidebar', () => {
  it('keeps the field open after the menu closes, and Enter saves the new title', async () => {
    const onRename = vi.fn().mockResolvedValue(undefined);
    render(<Harness onRename={onRename} />);
    await openRename();
    await settle();
    const field = (await screen.findByRole('textbox')) as HTMLInputElement;
    fireEvent.change(field, { target: { value: 'New name' } });
    fireEvent.keyDown(field, { key: 'Enter' });
    await waitFor(() => expect(onRename).toHaveBeenCalledWith('tab-a', 'New name'));
    await waitFor(() => expect(screen.getByText('New name')).toBeTruthy());
  });

  it('saves on blur and drops on Escape', async () => {
    const onRename = vi.fn().mockResolvedValue(undefined);
    render(<Harness onRename={onRename} />);
    await openRename();
    await settle();
    const field = (await screen.findByRole('textbox')) as HTMLInputElement;
    fireEvent.change(field, { target: { value: 'By blur' } });
    fireEvent.blur(field);
    await waitFor(() => expect(onRename).toHaveBeenCalledWith('tab-a', 'By blur'));
    await waitFor(() => expect(screen.getByText('By blur')).toBeTruthy());
  });

  it('does not bring the menu back open when the field is done, and leaves the row unmarked', async () => {
    const onRename = vi.fn().mockResolvedValue(undefined);
    render(<Harness onRename={onRename} />);
    await openRename(); // a tap on «…» mounts the menu open
    await settle();
    const field = (await screen.findByRole('textbox')) as HTMLInputElement;
    fireEvent.keyDown(field, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('textbox')).toBeNull());
    await settle();
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.querySelector('[data-menu]')).toBeNull();
    expect(onRename).not.toHaveBeenCalled();
  });
});
