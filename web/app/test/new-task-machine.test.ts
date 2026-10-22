import { beforeEach, describe, expect, it, vi } from 'vitest';

// The browser's storage, kept between "reloads" of the module like a real one is.
const stored = new Map<string, string>();
vi.stubGlobal('window', { addEventListener: () => {}, removeEventListener: () => {} });
vi.stubGlobal('localStorage', {
  getItem: (key: string) => stored.get(key) ?? null,
  setItem: (key: string, value: string) => void stored.set(key, value),
  removeItem: (key: string) => void stored.delete(key),
});

const load = async () => {
  vi.resetModules();
  return import('@/lib/new-task-machine');
};

describe('the machine picked for new tasks', () => {
  beforeEach(() => stored.clear());

  it('outlives a reload of the page', async () => {
    (await load()).setNewTaskMachine('runner-2');
    expect((await load()).newTaskMachine()).toBe('runner-2');
  });

  it('starts a design conversation there when that machine holds a copy of the project', async () => {
    const store = await load();
    store.setNewTaskMachine('runner-2');
    expect(
      store.designChatMachine({ machineId: 'dev-department', machineIds: ['dev-department', 'runner-2'] }),
    ).toBe('runner-2');
  });

  it('keeps the copy on screen when the picked machine has none', async () => {
    const store = await load();
    store.setNewTaskMachine('runner-2');
    expect(store.designChatMachine({ machineId: 'dev-department', machineIds: ['dev-department'] })).toBe(
      'dev-department',
    );
  });

  it('keeps the copy on screen when nothing is picked', async () => {
    const store = await load();
    expect(
      store.designChatMachine({ machineId: 'dev-department', machineIds: ['dev-department', 'runner-2'] }),
    ).toBe('dev-department');
  });

  it('forgets the pick when it is cleared', async () => {
    const store = await load();
    store.setNewTaskMachine('runner-2');
    store.setNewTaskMachine(null);
    expect((await load()).newTaskMachine()).toBeNull();
  });
});

describe('the latest conversation about a design', () => {
  const chat = (machineId: string, tabId: string) => ({ machineId, agent: { tabId } });
  const both = ['dev-department', 'runner-2'];

  beforeEach(() => stored.clear());

  it('is the newest one on the machine conversations start on', async () => {
    const store = await load();
    store.setNewTaskMachine('runner-2');
    const project = {
      machineId: 'dev-department',
      machineIds: both,
      chats: [chat('dev-department', 'a'), chat('runner-2', 'b'), chat('runner-2', 'c')],
    };
    expect(store.latestDesignChat(project)?.agent.tabId).toBe('b');
  });

  it('is none when that machine has no conversation about it yet', async () => {
    const store = await load();
    store.setNewTaskMachine('runner-2');
    expect(
      store.latestDesignChat({
        machineId: 'dev-department',
        machineIds: both,
        chats: [chat('dev-department', 'a')],
      }),
    ).toBeNull();
  });
});
