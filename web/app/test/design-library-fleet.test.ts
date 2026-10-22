import { describe, expect, it } from 'vitest';
import { mergeLibraries, type IMachineLibrary } from '@/lib/hl-design-library/fleet';
import type { IArtifactAgent, ILibraryArtifact } from '@/lib/hl-design-library/types';

const agent = (tabId: string, lastEventAt: number): IArtifactAgent => ({
  tabId,
  title: tabId,
  workspace: null,
  state: 'idle',
  lastEventAt,
  self: false,
});

const artifact = (id: string, updatedAt: number, agents: IArtifactAgent[] = []): ILibraryArtifact => ({
  id,
  title: id,
  source: null,
  updatedAt,
  boards: [],
  hasSupport: true,
  projectPath: `/srv/design/${id}/project`,
  cover: null,
  check: null,
  agents,
  claims: [],
});

const library = (machineId: string, port: number, artifacts: ILibraryArtifact[]): IMachineLibrary => ({
  machineId,
  machineName: machineId,
  data: { port, root: '/srv/design', selfTabId: null, artifacts },
});

describe('a project synced to several machines', () => {
  it('knows every machine that holds a copy, whichever copy it shows', () => {
    const merged = mergeLibraries(
      [
        library('dev-department', 18470, [artifact('analytics', 5)]),
        library('runner-2', 18471, [artifact('analytics', 9)]),
      ],
      'dev-department',
    );
    expect(merged).toHaveLength(1);
    expect(merged[0].machineId).toBe('dev-department');
    expect(merged[0].machineIds).toEqual(['dev-department', 'runner-2']);
  });

  it('gathers the conversations about it from every machine, newest first', () => {
    const merged = mergeLibraries(
      [
        library('dev-department', 18470, [artifact('analytics', 5, [agent('old', 1)])]),
        library('runner-2', 18471, [artifact('analytics', 9, [agent('new', 7)])]),
      ],
      'dev-department',
    );
    expect(merged[0].chats.map((chat) => [chat.machineId, chat.agent.tabId])).toEqual([
      ['runner-2', 'new'],
      ['dev-department', 'old'],
    ]);
  });
});
