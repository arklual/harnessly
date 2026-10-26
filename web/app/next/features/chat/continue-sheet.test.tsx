// @vitest-environment jsdom
import { createRef, type ReactNode } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import ContinueSheet from '@/next/features/chat/continue-sheet';
import type { IPickerAgent } from '@/next/features/composer/agents';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const model = (id: string, label: string) => ({ id, label, hint: '', defaultEffort: null, efforts: [] });
const agent = (over: Partial<IPickerAgent>): IPickerAgent =>
  ({
    provider: 'codex',
    name: 'Codex',
    short: 'Codex',
    models: [model('gpt', 'GPT-5.5')],
    canSwitch: true,
    available: true,
    status: 'ready',
    ...over,
  }) as IPickerAgent;

const KIMI = agent({
  provider: 'claude',
  profile: 'kimi',
  name: 'Kimi',
  short: 'Kimi',
  models: [model('k3', 'K3 Pro')],
});

// The sheet mounts into the interface's own root, as in the app.
const host = createRef<HTMLDivElement>();
const inRoot = (ui: ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

describe('continue in another model', () => {
  it('lists the models of every group and hands the picked one over', async () => {
    const onPick = vi.fn();
    inRoot(
      <ContinueSheet
        open
        onClose={() => undefined}
        agents={[agent({}), KIMI]}
        onPick={onPick}
        onConnect={vi.fn()}
      />,
    );
    expect(await screen.findByText('GPT-5.5')).toBeTruthy();
    fireEvent.click(await screen.findByRole('button', { name: 'K3 Pro' }));
    expect(onPick).toHaveBeenCalledWith(KIMI, KIMI.models[0]);
  });

  it('leads to the models and keys page', async () => {
    const onConnect = vi.fn();
    inRoot(
      <ContinueSheet open onClose={() => undefined} agents={[KIMI]} onPick={vi.fn()} onConnect={onConnect} />,
    );
    fireEvent.click(await screen.findByRole('button', { name: 'Connect models' }));
    expect(onConnect).toHaveBeenCalled();
  });
});
