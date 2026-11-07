import { describe, expect, it } from 'vitest';
import type { IInstalledApp } from '@/next/features/app-views/api';
import { backgroundApps } from '@/next/features/app-views/background';

const app = (id: string, more: Partial<IInstalledApp> = {}): IInstalledApp =>
  ({
    id,
    name: id,
    description: '',
    version: '1.0.0',
    trust: 'local',
    targets: ['pane'],
    permissions: ['background', 'hooks.agent'],
    views: [{ id: 'main', targets: ['pane'] }],
    mcpServers: [],
    ...more,
  }) as IInstalledApp;

describe('backgroundApps', () => {
  it('keeps only the apps on in the chat that hold background and are agreed to and on', () => {
    const installed = [
      app('a.on'),
      app('b.notinchat'),
      app('c.noright', { permissions: ['hooks.agent'] }),
      app('d.off', { off: true }),
      app('e.muted', { muted: true }),
      app('f.consent', { consent: true }),
      app('g.nopane', { views: [{ id: 'card', targets: ['chat'] }] as IInstalledApp['views'] }),
    ];
    const on = ['a.on', 'c.noright', 'd.off', 'e.muted', 'f.consent', 'g.nopane'];
    expect(backgroundApps(installed, on).map((a) => a.id)).toEqual(['a.on']);
  });
});
