import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { ProjectPage } from '@/components/agents/design/library/project-page';
import { upgradeDesignRelease } from '@/lib/hl-design-library-client';
import type { ILibraryProject } from '@/lib/hl-design-library/fleet';
import type { ILibraryProjectRelease } from '@/lib/hl-design-library/types';

const project = (release?: ILibraryProjectRelease): ILibraryProject => ({
  id: 'admin',
  title: 'Admin',
  source: null,
  updatedAt: 1,
  boards: [],
  hasSupport: true,
  projectPath: '/srv/design/admin/project',
  cover: null,
  check: null,
  agents: [],
  claims: [],
  ...(release ? { release } : {}),
  machineId: 'runner-2',
  machineName: 'runner-2',
  port: 18470,
  machineIds: ['runner-2'],
  chats: [],
});

const render = (p: ILibraryProject) =>
  renderToStaticMarkup(createElement(ProjectPage, { project: p, onBack: () => {}, onOpenBoard: () => {} }));

describe('upgrading a design project to a newer release', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('offers the newer stable release the runtime names', () => {
    const html = render(
      project({ current: 'legacy', pinned: false, upgrade: '1792547779-0491', skill: null }),
    );
    expect(html).toContain('1792547779-0491');
    expect(html).toContain('Upgrade to release');
  });

  it('offers nothing when the project is on the newest release or the machine keeps no mirror', () => {
    expect(
      render(project({ current: '1792547779-0491', pinned: true, upgrade: null, skill: null })),
    ).not.toContain('Upgrade to release');
    expect(render(project())).not.toContain('Upgrade to release');
  });

  it("asks the project's own machine to move it, by an explicit POST", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            configured: true,
            releases: [],
            newest: 'r2',
            newProjects: 'r2',
            chosen: null,
            project: { current: 'r2', pinned: true, upgrade: null, skill: null },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
    );
    vi.stubGlobal('fetch', fetchMock);
    const answer = await upgradeDesignRelease('runner-2', 'admin', 'r2');
    expect(answer.project?.current).toBe('r2');
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toContain('/api/platform/design/library/releases');
    expect(url).toContain('machine=runner-2');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ artifact: 'admin', release: 'r2' });
  });

  it("shows the runtime's refusal", async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          new Response(JSON.stringify({ error: 'Release r3 has not passed the check' }), { status: 422 }),
      ),
    );
    await expect(upgradeDesignRelease(undefined, 'admin', 'r3')).rejects.toThrow(
      'Release r3 has not passed the check',
    );
  });
});
