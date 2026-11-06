// @vitest-environment jsdom
import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { IInstalledApps } from '@/next/features/app-views/api';
import { applyBranding, brandingOf, slotViews } from '@/next/features/app-views/slots';
import { SlotBoundary } from '@/next/features/app-views/ui-slot';

afterEach(cleanup);

const Crash = ({ when }: { when: { on: boolean } }) => {
  if (when.on) throw new Error('extension crashed');
  return <span>extension ok</span>;
};

describe('a crashing extension', () => {
  it('is replaced by a line that says so, and the page around it stays', () => {
    const quiet = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const state = { on: true };
    const retry = vi.fn();
    const { getByText, getByRole } = render(
      <main>
        <h1>Settings</h1>
        <SlotBoundary onRetry={retry}>
          <Crash when={state} />
        </SlotBoundary>
        <p>the rest of the page</p>
      </main>,
    );
    expect(getByText('Settings')).toBeTruthy();
    expect(getByText('the rest of the page')).toBeTruthy();
    expect(getByRole('alert').textContent).toContain('extension crashed');
    state.on = false;
    fireEvent.click(getByRole('button'));
    expect(retry).toHaveBeenCalledTimes(1);
    expect(getByText('extension ok')).toBeTruthy();
    quiet.mockRestore();
  });
});

const view = (id: string, targets: string[], more: object = {}) => ({ id, targets, ...more });
const app = (id: string, views: object[], more: object = {}) => ({
  id,
  name: id,
  description: '',
  version: '1.0.0',
  trust: 'sandboxed',
  targets: [],
  permissions: [],
  mcpServers: [],
  views,
  ...more,
});
const list = (slots: boolean): IInstalledApps =>
  ({
    viewPort: 1,
    agentWalled: false,
    slots,
    apps: [
      app(
        'example-corp.b',
        [view('act', ['object-action'], { objects: ['machine'], order: 5 }), view('set', ['settings'])],
        { branding: { name: 'Example Corp Agents', accent: '#2f6fed' } },
      ),
      app('example-corp.a', [
        view('act', ['object-action'], { objects: ['chat'] }),
        view('st', ['onboarding']),
      ]),
    ],
  }) as unknown as IInstalledApps;

describe('slot views', () => {
  it('are empty while the slots are off', () => {
    expect(slotViews(list(false), 'settings')).toEqual([]);
    expect(brandingOf(list(false))).toBeNull();
    expect(slotViews(null, 'settings')).toEqual([]);
  });

  it('are picked by slot, object kind and order', () => {
    expect(slotViews(list(true), 'settings').map((s) => s.view.id)).toEqual(['set']);
    expect(slotViews(list(true), 'object-action', 'machine').map((s) => s.app.id)).toEqual([
      'example-corp.b',
    ]);
    expect(slotViews(list(true), 'object-action', 'chat').map((s) => s.app.id)).toEqual(['example-corp.a']);
    expect(slotViews(list(true), 'onboarding').length).toBe(1);
  });

  it('brand the page and give it back', () => {
    const b = brandingOf(list(true));
    expect(b?.name).toBe('Example Corp Agents');
    const before = document.title;
    const undo = applyBranding({ name: 'Example Corp Agents', accent: '#2f6fed' });
    expect(document.title).toBe('Example Corp Agents');
    expect(document.documentElement.style.getPropertyValue('--hx-accent')).toBe('#2f6fed');
    undo();
    expect(document.title).toBe(before);
    expect(document.documentElement.style.getPropertyValue('--hx-accent')).toBe('');
  });

  it('do not take an accent that is not a hex colour', () => {
    const undo = applyBranding({ name: 'X', accent: 'url(javascript:1)' });
    expect(document.documentElement.style.getPropertyValue('--hx-accent')).toBe('');
    undo();
  });
});
