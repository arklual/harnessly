import { bp, profileName } from './profile-fields';
// @vitest-environment jsdom
import {
  act,
  cleanup,
  fireEvent,
  render as testingRender,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useState, type ReactNode, type ReactElement } from 'react';
import { PortalContext } from '@/next/ui/portal';
import { pinLocale } from '@/next/i18n';
import type { IProfilesPage } from './profiles-api';
const api = vi.hoisted(() => ({
  loadProfiles: vi.fn(),
  loadConversationProfiles: vi.fn(),
  changeProfile: vi.fn(),
  previewProjectAccess: vi.fn(),
}));
vi.mock('./profiles-api', () => api);
vi.mock('@/next/ui/use-media', () => ({ usePhone: () => false }));
vi.mock('@/next/stores/projects', () => ({ useProjects: () => [{ id: 'project-a', name: 'Project A' }] }));
import { ProfilePicker } from './profile-picker';
import { BrowserProfileAskHost } from '@/next/bindings/browser-profile-asks';
import { BrowserPage } from '@/next/bindings/settings-browser';
import { useProjectBrowserSettings } from './project-browser-settings';
import ProjectSettingsDialog from '@/next/features/sidebar/project-settings-dialog';
const p1 = {
  id: 'p1',
  name: 'Work',
  color: '#112233',
  kind: 'managed',
  createdAt: 1,
  lastUsedAt: 0,
  usedBy: [],
  path: '/data/browser/p1',
  sizeBytes: 64_000_000,
  running: true,
  projects: ['project-a'],
};
const p2 = { ...p1, id: 'p2', name: 'Other', path: '/data/browser/p2', sizeBytes: undefined, projects: [] };
const page = (): IProfilesPage => ({
  profiles: [p1, p2],
  projects: { 'project-a': { default: p1.id, granted: [] } },
  asks: [],
  noSandbox: false,
});
let portal: HTMLDivElement;
const render = (ui: ReactElement) =>
  testingRender(ui, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <PortalContext value={{ current: portal }}>{children}</PortalContext>
    ),
  });
beforeEach(() => {
  pinLocale('en');
  vi.resetAllMocks();
  portal = document.createElement('div');
  document.body.append(portal);
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
  api.loadProfiles.mockResolvedValue(page());
  api.loadConversationProfiles.mockResolvedValue({ profiles: [p1, p2], current: p1, hasTabs: true });
  api.changeProfile.mockResolvedValue(undefined);
  api.previewProjectAccess.mockResolvedValue(0);
});
afterEach(() => {
  cleanup();
  portal.remove();
  vi.useRealTimers();
  pinLocale(null);
});
it('confirms closing own tabs, pauses before switching, and resumes after a server refusal', async () => {
  const pause = vi.fn(),
    resume = vi.fn();
  render(<ProfilePicker machine="m" conv="chat-a" onPause={pause} onResume={resume} />);
  const select = screen.getByRole('button', { name: 'Browser profile' });
  await waitFor(() => expect(select.textContent).toContain('Work'));
  fireEvent.click(select);
  fireEvent.click(await screen.findByRole('menuitemradio', { name: 'Other' }));
  await screen.findByRole('dialog');
  expect(api.changeProfile).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(pause).not.toHaveBeenCalled();
  api.changeProfile.mockImplementation(async () => {
    expect(pause).toHaveBeenCalledOnce();
    throw new Error('Access was revoked');
  });
  fireEvent.click(select);
  fireEvent.click(await screen.findByRole('menuitemradio', { name: 'Other' }));
  await screen.findByRole('dialog');
  fireEvent.click(screen.getByRole('button', { name: 'Switch profile' }));
  await waitFor(() => expect(resume).toHaveBeenCalledOnce());
  expect(api.changeProfile).toHaveBeenCalledWith('m', { action: 'use', conv: 'chat-a', id: 'p2' });
  expect(await screen.findByLabelText('Access was revoked')).toBeTruthy();
  expect(select.textContent).toContain('Work');
});
it('rechecks tabs at selection time and switches without confirmation when none are open', async () => {
  api.loadConversationProfiles.mockResolvedValue({ profiles: [p1, p2], current: p1, hasTabs: false });
  render(<ProfilePicker machine="m" conv="chat" onPause={vi.fn()} onResume={vi.fn()} />);
  const select = screen.getByRole('button', { name: 'Browser profile' });
  await waitFor(() => expect(select.textContent).toContain('Work'));
  fireEvent.click(select);
  fireEvent.click(await screen.findByRole('menuitemradio', { name: 'Other' }));
  await waitFor(() => expect(api.changeProfile).toHaveBeenCalledOnce());
  expect(api.loadConversationProfiles.mock.calls.length).toBeGreaterThan(1);
  expect(screen.queryByRole('dialog')).toBeNull();
});
it.each(['Only this chat', 'Whole project', 'Decline'])(
  'answers the global access card with %s while the browser panel is absent',
  async (button) => {
    api.loadProfiles.mockResolvedValue({
      ...page(),
      asks: [{ id: 'ask', conv: 'chat-a', title: 'Account task', why: 'Need the staging account', at: 1 }],
    });
    render(<BrowserProfileAskHost machine="m" />);
    await screen.findByText('Account task');
    expect(screen.getByText('Need the staging account')).toBeTruthy();
    if (button === 'Only this chat') {
      fireEvent.change(screen.getByRole('combobox'), { target: { value: '' } });
      fireEvent.change(screen.getByLabelText('Profile name'), { target: { value: 'Staging' } });
    }
    fireEvent.click(screen.getByRole('button', { name: button }));
    await waitFor(() => expect(api.changeProfile).toHaveBeenCalledOnce());
    expect(api.changeProfile).toHaveBeenCalledWith(
      'm',
      expect.objectContaining({
        action: 'answer',
        askId: 'ask',
        scope: button === 'Decline' ? 'decline' : button === 'Whole project' ? 'project' : 'chat',
      }),
    );
    if (button === 'Only this chat')
      expect(api.changeProfile.mock.calls[0]![1]).toMatchObject({
        id: '',
        name: 'Staging',
        color: '#6688cc',
      });
  },
);
it('ignores a late response from the previous machine', async () => {
  let finish: (value: IProfilesPage) => void = () => undefined;
  api.loadProfiles.mockImplementation((machine: string) =>
    machine === 'old'
      ? new Promise<IProfilesPage>((resolve) => {
          finish = resolve;
        })
      : Promise.resolve(page()),
  );
  const { rerender } = render(<BrowserProfileAskHost machine="old" />);
  rerender(<BrowserProfileAskHost machine="new" />);
  await act(async () =>
    finish({
      ...page(),
      asks: [{ id: 'old-ask', conv: 'old-chat', title: 'Old machine task', why: 'old reason', at: 1 }],
    }),
  );
  expect(screen.queryByText('Old machine task')).toBeNull();
});
it('requires confirmation listing affected projects before deletion and before disabling sandbox', async () => {
  render(<BrowserPage machine="m" now={1000} />);
  await screen.findByTitle('/data/browser/p1');
  const row = screen.getByTitle('/data/browser/p1').closest('article')!;
  fireEvent.click(within(row).getByRole('button', { name: 'Delete' }));
  const dialog = await screen.findByRole('dialog');
  expect(within(dialog).getByText('Projects: Project A')).toBeTruthy();
  expect(api.changeProfile).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(api.changeProfile).toHaveBeenCalledWith('m', { action: 'delete', id: 'p1' }));
  fireEvent.click(screen.getByLabelText('Disable Chrome sandbox'));
  await screen.findByRole('dialog');
  expect(api.changeProfile).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  await waitFor(() =>
    expect(api.changeProfile).toHaveBeenCalledWith('m', { action: 'sandbox', noSandbox: true }),
  );
});
function ProjectForm({
  onSave,
  onClose,
}: {
  onSave: (change: { name?: string; path?: string }) => Promise<void>;
  onClose: () => void;
}) {
  const browser = useProjectBrowserSettings('m', 'project-a');
  const [project, setProject] = useState({ name: 'Project A', path: '/workspace' });
  return (
    <ProjectSettingsDialog
      project={project}
      folders={[]}
      browserSettings={browser}
      chats={{ count: 0, working: 0 }}
      onClose={onClose}
      onSave={async (change) => {
        await onSave(change);
        setProject((p) => ({ ...p, ...change }));
      }}
    />
  );
}
it('uses the single project Save button for browser-only changes', async () => {
  const save = vi.fn(),
    close = vi.fn();
  render(<ProjectForm onSave={save} onClose={close} />);
  const select = await screen.findByRole('combobox', { name: 'Default profile' });
  fireEvent.change(select, { target: { value: '' } });
  fireEvent.click(screen.getByLabelText('Other'));
  expect(screen.getAllByRole('button', { name: 'Save' })).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  expect(api.changeProfile).toHaveBeenCalledWith('m', {
    action: 'project',
    project: 'project-a',
    default: '',
    granted: ['p2'],
  });
  expect(save).not.toHaveBeenCalled();
  expect(screen.queryByRole('dialog', { name: 'Change project browser access?' })).toBeNull();
});
it('confirms the affected chat count before either project or browser writes and supports cancellation', async () => {
  const save = vi.fn(),
    close = vi.fn();
  api.previewProjectAccess.mockResolvedValue(2);
  render(<ProjectForm onSave={save} onClose={close} />);
  fireEvent.change(await screen.findByRole('combobox', { name: 'Default profile' }), {
    target: { value: 'p2' },
  });
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Renamed' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  const dialog = await screen.findByRole('dialog', { name: 'Change project browser access?' });
  expect(within(dialog).getByText('Chats whose browser tabs will close: 2. Continue saving?')).toBeTruthy();
  expect(save).not.toHaveBeenCalled();
  expect(api.changeProfile).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(false),
  );
  expect(close).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  fireEvent.click(
    within(await screen.findByRole('dialog', { name: 'Change project browser access?' })).getByRole(
      'button',
      { name: 'Confirm' },
    ),
  );
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  expect(save).toHaveBeenCalledWith({ name: 'Renamed' });
  expect(api.changeProfile).toHaveBeenCalledWith('m', {
    action: 'project',
    project: 'project-a',
    default: 'p2',
    granted: [],
  });
});
it.each(['project', 'browser'])(
  'keeps the dialog and browser draft when the %s write fails',
  async (part) => {
    const save = vi.fn(),
      close = vi.fn();
    if (part === 'project') save.mockRejectedValueOnce(new Error('Project is unavailable'));
    else api.changeProfile.mockRejectedValueOnce(new Error('Access was revoked'));
    render(<ProjectForm onSave={save} onClose={close} />);
    fireEvent.change(await screen.findByRole('combobox', { name: 'Default profile' }), {
      target: { value: 'p2' },
    });
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Renamed' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain(
      part === 'project' ? 'Project is unavailable' : 'Browser access was not saved: Access was revoked',
    );
    expect(close).not.toHaveBeenCalled();
    expect((screen.getByRole('combobox', { name: 'Default profile' }) as HTMLSelectElement).value).toBe('p2');
    if (part === 'project') expect(api.changeProfile).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(close).toHaveBeenCalledOnce());
    // A successful project write is not repeated when retrying only failed browser access.
    expect(save).toHaveBeenCalledTimes(part === 'project' ? 2 : 1);
  },
);
it('identifies a project timeout and never starts browser access after it', async () => {
  const close = vi.fn();
  render(<ProjectForm onSave={() => new Promise<void>(() => undefined)} onClose={close} />);
  fireEvent.change(await screen.findByRole('combobox', { name: 'Default profile' }), {
    target: { value: 'p2' },
  });
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Renamed' } });
  vi.useFakeTimers();
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await act(async () => {
    await Promise.resolve();
  });
  await act(async () => vi.advanceTimersByTimeAsync(13_000));
  expect(screen.getByRole('alert').textContent).toBe(
    'Could not save the project: The server did not answer in time',
  );
  expect(api.changeProfile).not.toHaveBeenCalled();
  expect(close).not.toHaveBeenCalled();
});
it('shows disk usage, keeps the path in a tooltip, and warns about login loss before stopping', async () => {
  render(<BrowserPage machine="m" now={1000} />);
  const size = await screen.findByTitle('/data/browser/p1');
  expect(size.textContent).toBe('64 MB');
  expect(screen.getByTitle('/data/browser/p2').textContent).toBe('—');
  expect(screen.queryByText('/data/browser/p1')).toBeNull();
  fireEvent.click(within(size.closest('article')!).getByRole('button', { name: 'Stop' }));
  const dialog = await screen.findByRole('dialog', { name: 'Stop browser profile?' });
  expect(
    within(dialog).getByText(/Sites that keep you signed in only until the browser closes/),
  ).toBeTruthy();
  expect(api.changeProfile).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => expect(api.changeProfile).toHaveBeenCalledWith('m', { action: 'stop', id: 'p1' }));
});
it('polls asks only while visible, and reloads immediately when the page becomes visible', async () => {
  vi.useFakeTimers();
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
  render(<BrowserProfileAskHost machine="m" />);
  await act(async () => vi.advanceTimersByTime(9000));
  expect(api.loadProfiles).not.toHaveBeenCalled();
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
  await act(async () => document.dispatchEvent(new Event('visibilitychange')));
  expect(api.loadProfiles).toHaveBeenCalledOnce();
  await act(async () => vi.advanceTimersByTime(3000));
  expect(api.loadProfiles).toHaveBeenCalledTimes(2);
});
it('keeps an import path and a rename draft after a server error', async () => {
  render(<BrowserPage machine="m" now={1000} />);
  await screen.findByTitle('/data/browser/p1');
  fireEvent.change(screen.getByLabelText('Profile source'), { target: { value: 'import' } });
  fireEvent.change(screen.getByLabelText('Profile name'), { target: { value: 'Imported' } });
  fireEvent.change(screen.getByLabelText('Disk path'), { target: { value: '/data/existing-chrome' } });
  api.changeProfile.mockRejectedValueOnce(new Error('Close Chrome first'));
  fireEvent.click(screen.getByRole('button', { name: 'Import Chrome folder' }));
  await screen.findByText('Close Chrome first');
  expect((screen.getByLabelText('Disk path') as HTMLInputElement).value).toBe('/data/existing-chrome');
  const row = screen.getByTitle('/data/browser/p1').closest('article')!;
  fireEvent.click(within(row).getByRole('button', { name: 'Rename / color' }));
  fireEvent.change(within(row).getByLabelText('Profile name'), { target: { value: 'Renamed' } });
  fireEvent.click(within(row).getByRole('button', { name: 'Save' }));
  await waitFor(() =>
    expect(api.changeProfile).toHaveBeenCalledWith('m', {
      action: 'update',
      id: 'p1',
      name: 'Renamed',
      color: '#112233',
    }),
  );
});
it('displays a successful import warning after clearing the form', async () => {
  api.changeProfile.mockResolvedValueOnce({ warning: 'Some saved logins could not be copied' });
  render(<BrowserPage machine="m" now={1000} />);
  await screen.findByTitle('/data/browser/p1');
  fireEvent.change(screen.getByLabelText('Profile source'), { target: { value: 'import' } });
  fireEvent.change(screen.getByLabelText('Profile name'), { target: { value: 'Imported' } });
  fireEvent.change(screen.getByLabelText('Disk path'), { target: { value: '/data/existing-chrome' } });
  fireEvent.click(screen.getByRole('button', { name: 'Import Chrome folder' }));
  await screen.findByText(bp('importNote'));
  expect(screen.queryByText('Some saved logins could not be copied')).toBeNull();
  expect((screen.getByLabelText('Profile name') as HTMLInputElement).value).toBe('');
});

it('localizes only the legacy common label while preserving explicit profile names', () => {
  pinLocale('ru');
  expect(profileName({ id: 'common', name: 'Common' })).toBe('Общий');
  expect(profileName({ id: 'common', name: 'Personal' })).toBe('Personal');
  expect(profileName({ id: 'other', name: 'Common' })).toBe('Common');
  pinLocale('en');
  expect(profileName({ id: 'common', name: 'Common' })).toBe('Common');
});
