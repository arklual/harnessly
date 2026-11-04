// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { ProjectSafeAccess, ProjectSecrets } from '@/next/bindings/project-safe-access';
import { setFetch } from '@/next/core/net/http';
import { readSecret } from '@/next/features/safe/api';
import { pinLocale } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

const MACHINE = 'local' as MachineId;
const NOW = Date.UTC(2026, 9, 27, 12);
const wire = (over: Record<string, unknown> = {}) => ({
  name: 'github-token',
  createdAt: NOW - 3600_000,
  updatedAt: NOW - 3600_000,
  lastUsedAt: NOW - 300_000,
  usedBy: [{ conv: 'c-1', title: 'Release notes', at: NOW - 300_000 }],
  ...over,
});
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
beforeAll(() => pinLocale('en'));
afterEach(() => {
  cleanup();
  setFetch((url, init) => fetch(url, init));
});

describe('project Access secrets', () => {
  it('shows only granted secrets with origins, recent uses and direct edit links', () => {
    render(
      <ProjectSecrets
        project="p-1"
        now={NOW}
        secrets={[
          readSecret(
            wire({
              origins: ['https://example.org'],
              projects: ['p-1'],
              usedBy: [
                { conv: 'c-1', title: 'Release notes', at: NOW - 60_000, origin: 'https://example.org' },
              ],
            }),
          ),
          readSecret(wire({ name: 'another-project', projects: ['p-2'] })),
          readSecret(wire({ name: 'unscoped' })),
        ]}
      />,
    );
    expect(screen.getByText('github-token')).toBeTruthy();
    expect(screen.queryByText('another-project')).toBeNull();
    expect(screen.getByText('unscoped')).toBeTruthy();
    expect(screen.getAllByText('Latest uses')).toHaveLength(2);
    expect(screen.getByText(/Release notes · https:\/\/example.org/)).toBeTruthy();
    expect(screen.getAllByRole('link', { name: 'Edit access' })[0]!.getAttribute('href')).toBe(
      '/settings/safe?secret=github-token',
    );
    expect(screen.queryByRole('button')).toBeNull();
    expect(document.querySelector('input')).toBeNull();
  });

  it('retries failed reads and shows the empty state', async () => {
    let fails = true;
    setFetch(async () => (fails ? json(500, { error: 'failed' }) : json(200, { secrets: [], asks: [] })));
    render(<ProjectSafeAccess machine={MACHINE} project="p-1" />);
    await screen.findByText('The machine could not write the safe');
    fails = false;
    fireEvent.click(screen.getByRole('button'));
    expect(await screen.findByText('No secrets available to this project yet')).toBeTruthy();
  });
});
