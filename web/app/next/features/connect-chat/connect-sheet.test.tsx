// @vitest-environment jsdom
import { createRef, type ReactNode } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import ConnectSheet from '@/next/features/connect-chat/connect-sheet';
import type { IConnectChat } from '@/next/features/connect-chat/api';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const host = createRef<HTMLDivElement>();
const inRoot = (ui: ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

const state = (over: Partial<IConnectChat> = {}): IConnectChat => ({
  on: null,
  cwd: '/home/ubuntu/proj',
  claude: true,
  busy: false,
  devices: [
    { id: 'd-1', name: 'omarchy', os: 'linux', online: true, suggest: '/home/me/Projects/proj' },
    { id: 'd-2', name: 'mac', os: 'darwin', online: false },
  ],
  ...over,
});
const open = (data: IConnectChat | null) => {
  const onSwitch = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn();
  inRoot(<ConnectSheet open onClose={onClose} load={async () => data} onSwitch={onSwitch} />);
  return { onSwitch, onClose };
};
const send = () => screen.getByRole('button', { name: 'Switch' });

describe('where the chat runs', () => {
  it('switches to a computer with the folder it suggests, and an offline one cannot be picked', async () => {
    const { onSwitch, onClose } = open(state());
    await waitFor(() => expect(screen.getByRole('radio', { name: /omarchy/ })).toBeTruthy());
    expect(send()).toHaveProperty('disabled', true);
    expect(screen.getByRole('radio', { name: /^mac/ })).toHaveProperty('disabled', true);
    fireEvent.click(screen.getByRole('radio', { name: /omarchy/ }));
    expect(screen.getByRole('textbox')).toHaveProperty('value', '/home/me/Projects/proj');
    fireEvent.click(send());
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onSwitch).toHaveBeenCalledWith({ device: 'd-1', dir: '/home/me/Projects/proj' });
  });

  it('puts a switched chat back on this machine', async () => {
    const { onSwitch } = open(state({ on: { device: 'd-1', dir: '/home/me/x' } }));
    await waitFor(() => expect(screen.getByRole('radio', { name: /This machine/ })).toBeTruthy());
    fireEvent.click(screen.getByRole('radio', { name: /This machine/ }));
    fireEvent.click(send());
    await waitFor(() => expect(onSwitch).toHaveBeenCalledWith(null));
  });

  it('says why a chat of another agent cannot go', async () => {
    open(state({ claude: false }));
    await waitFor(() => expect(screen.getByText(/Only Claude chats/)).toBeTruthy());
    expect(send()).toHaveProperty('disabled', true);
  });
});
