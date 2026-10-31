// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createRef } from 'react';
import { PortalContext } from '@/next/ui/portal';
import ProjectSettingsDialog, {
  type IProjectSettingsDialogProps,
} from '@/next/features/sidebar/project-settings-dialog';

afterEach(cleanup);

// The dialog mounts into the interface's own root, as in the app.
const host = createRef<HTMLDivElement>();
const open = (props: Partial<IProjectSettingsDialogProps>) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      <ProjectSettingsDialog
        project={{ name: 'Витрина', path: '/w/shop' }}
        folders={[]}
        chats={{ count: 3, working: 0 }}
        onClose={vi.fn()}
        onSave={vi.fn()}
        {...props}
      />
    </PortalContext.Provider>,
  );

describe('deleting a project', () => {
  it('moves the chats out to the common list unless deleting them is picked', async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    open({ onDelete, onClose });
    fireEvent.click(await screen.findByRole('button', { name: 'Delete project' }));
    expect(await screen.findByText('The project has 3 chats. What becomes of them?')).toBeTruthy();
    const keep = screen.getByRole('radio', { name: /Move the chats/ });
    expect(keep.getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onDelete).toHaveBeenCalledWith('keep');
  });

  it('deletes the chats with it when picked, and says that working agents stop', async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    open({ onDelete, chats: { count: 2, working: 1 } });
    fireEvent.click(await screen.findByRole('button', { name: 'Delete project' }));
    fireEvent.click(await screen.findByRole('radio', { name: /Delete with its chats/ }));
    expect(screen.getByText(/1 agent is working now and will stop/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Delete with its chats' }));
    await waitFor(() => expect(onDelete).toHaveBeenCalledWith('delete'));
  });

  it('asks nothing of a project with no chats, and says why when the runtime refuses', async () => {
    const onDelete = vi.fn().mockRejectedValue(new Error('Not found'));
    const onClose = vi.fn();
    open({ onDelete, onClose, chats: { count: 0, working: 0 } });
    fireEvent.click(await screen.findByRole('button', { name: 'Delete project' }));
    expect(screen.queryByRole('radio')).toBeNull();
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Not found'));
    expect(onDelete).toHaveBeenCalledWith('keep');
    expect(onClose).not.toHaveBeenCalled();
  });

  it('goes back to the settings, and has no way to delete without onDelete', async () => {
    const { unmount } = open({ onDelete: vi.fn() });
    fireEvent.click(await screen.findByRole('button', { name: 'Delete project' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Back' }));
    expect(await screen.findByRole('button', { name: 'Save' })).toBeTruthy();
    unmount();
    open({});
    await screen.findByRole('button', { name: 'Save' });
    expect(screen.queryByRole('button', { name: 'Delete project' })).toBeNull();
  });
});
