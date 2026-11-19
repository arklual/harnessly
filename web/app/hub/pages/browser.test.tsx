// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { BrowserScreen, browserSocketUrl } from './browser';

const mocks = vi.hoisted(() => ({
  instances: [] as Array<{
    url: string;
    scaleViewport: boolean;
    disconnect: ReturnType<typeof vi.fn>;
  }>,
}));

vi.mock('@novnc/novnc', () => ({
  default: class MockRFB {
    url: string;
    scaleViewport = false;
    disconnect = vi.fn();

    constructor(_target: HTMLElement, url: string) {
      this.url = url;
      mocks.instances.push(this);
    }

    addEventListener() {}
  },
}));

beforeEach(() => {
  mocks.instances.length = 0;
});
afterEach(cleanup);

describe('BrowserScreen', () => {
  it('builds a same-host secure socket URL and escapes the machine id', () => {
    expect(browserSocketUrl('machine/one', { protocol: 'https:', host: 'hub.example.org' })).toBe(
      'wss://hub.example.org/api/machines/machine%2Fone/browser/ws',
    );
    expect(browserSocketUrl('machine-one', { protocol: 'http:', host: 'localhost:8022' })).toBe(
      'ws://localhost:8022/api/machines/machine-one/browser/ws',
    );
  });

  it('connects once under StrictMode and scales the desktop to fit', async () => {
    render(
      <StrictMode>
        <BrowserScreen machineId="machine-one" />
      </StrictMode>,
    );
    await act(async () => Promise.resolve());

    expect(mocks.instances).toHaveLength(1);
    expect(mocks.instances[0].url).toBe(`ws://${window.location.host}/api/machines/machine-one/browser/ws`);
    expect(mocks.instances[0].scaleViewport).toBe(true);
    expect(screen.getByRole('status')).toBeTruthy();
  });

  it('disconnects on unmount', async () => {
    const view = render(<BrowserScreen machineId="machine-one" />);
    await act(async () => Promise.resolve());

    view.unmount();
    expect(mocks.instances[0].disconnect).toHaveBeenCalledOnce();
  });
});
