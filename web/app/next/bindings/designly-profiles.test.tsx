// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { ComponentProps } from 'react';
import type DesignlyHome from '@/next/features/designly/designly-home';
import type { MachineId } from '@/next/protocol/v2';

const { create, agents } = vi.hoisted(() => ({
  create: vi.fn(async () => ({ id: 'design-1', conv: 'tab-1', title: 'Test' })),
  agents: [
    {
      provider: 'claude',
      status: 'ready',
      models: [{ id: 'sonnet', label: 'Sonnet', efforts: [], defaultEffort: null }],
      profiles: ['glm', 'kimi', 'gpt-compatible'].map((id) => ({
        id,
        label: id,
        vendor: 'glm',
        configured: true,
        models: [{ id: 'vendor/model', label: id, efforts: ['high'], defaultEffort: 'high' }],
      })),
    },
  ],
}));
vi.mock('@/next/stores/machines', () => ({ useMachine: () => ({ machine: { agents } }) }));
vi.mock('@/next/bindings/settings-data', () => ({
  useAgentSettings: () => ({ state: { k: 'ready', value: { options: [] } } }),
}));
vi.mock('@/next/features/tour/use-tour-step', () => ({ useTourStep: () => null }));
vi.mock('@/next/features/tour/use-tour-fill', () => ({ useTourFill: () => undefined }));
vi.mock('@/next/stores/tour', () => ({ tourFact: () => undefined }));
vi.mock('@/next/stores/connection', () => ({
  useLinkRestored: () => 0,
  onReconnectNow: () => () => undefined,
}));
vi.mock('@/next/stores/conversations', () => ({ useConversationList: () => [] }));
vi.mock('@/next/bindings/use-voice', () => ({ useVoice: () => ({ recording: false }) }));
vi.mock('@/next/bindings/use-uploads', () => ({ useUploads: () => ({ reset: () => undefined }) }));
vi.mock('@/next/routes/routes', () => ({ ROUTE: { site: 'site' }, goTo: () => undefined }));
vi.mock('@/next/features/app-catalog/api', () => ({
  loadKinds: async () => [],
  loadKindDocs: async () => [],
}));
vi.mock('@/next/features/designly/kit/kit-api', () => ({
  fetchKitCards: async () => [],
  shelfCards: () => [],
  kitCover: () => '',
}));
vi.mock('@/next/features/designly/api', async (original) => ({
  ...(await original<typeof import('@/next/features/designly/api')>()),
  fetchLibrary: async () => ({ designs: [], port: 0, root: '/designs' }),
  fetchSystems: async () => [],
  fetchLayout: async () => ({ folders: [], placed: {}, systems: {} }),
  createDesign: create,
}));
// Exercise the real binding and selection hook; replace only the presentation of the create form.
vi.mock('@/next/features/designly/designly-home', () => ({
  DESIGN_KIND: { site: 'site', slides: 'slides', ds: 'ds', video: 'video' },
  default: ({ compose }: ComponentProps<typeof DesignlyHome>) => (
    <div>
      {compose?.picker?.agents.map((a) => (
        <button
          key={a.profile ?? a.provider}
          onClick={() =>
            compose.picker?.onChange({
              provider: a.provider,
              profile: a.profile,
              model: a.models[0].id,
              effort: 'high',
              ultracode: false,
            })
          }
        >
          {a.profile ?? a.provider}
        </button>
      ))}
      <button
        onClick={() =>
          compose?.onCreate({
            brief: 'Test',
            kind: 'site',
            designSystem: null,
            deck: null,
            video: null,
            attachmentIds: [],
          })
        }
      >
        Create
      </button>
    </div>
  ),
}));
import DesignlyView from './designly';
beforeEach(() => {
  localStorage.clear();
  create.mockClear();
});
afterEach(cleanup);
it.each(['glm', 'kimi', 'gpt-compatible'])(
  'offers and forwards the %s profile without changing the CLI provider',
  async (profile) => {
    render(<DesignlyView machine={'m-profile' as MachineId} />);
    fireEvent.click(screen.getByRole('button', { name: profile }));
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(
        'm-profile',
        expect.objectContaining({ provider: 'claude', profile, model: 'vendor/model', effort: 'high' }),
      ),
    );
  },
);
