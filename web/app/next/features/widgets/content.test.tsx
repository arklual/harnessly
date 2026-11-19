// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const runtimeJson = vi.fn();
vi.mock('@/next/core/net/runtime-json', async (real) => ({
  ...(await real<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: (...a: unknown[]) => runtimeJson(...a),
}));

const { keepContent, readKeptContent, useWidgetContent } = await import('@/next/features/widgets/content');

/** The script the host's package puts in every widget's page to take a new version of its markup (package.go). */
const patchJS = (() => {
  const src = readFileSync(resolve(process.cwd(), '../internal/sidewidget/package.go'), 'utf8');
  const at = src.indexOf('const patchJS = `');
  return src.slice(at + 'const patchJS = `'.length, src.indexOf('`', at + 20));
})();

describe('a widget updated in place (HRN-1665)', () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.body.innerHTML =
      '<div id="hl-widget" data-rev="r1"><b id="n">10%</b></div><style id="hl-widget-css"></style>';
  });
  afterEach(() => runtimeJson.mockReset());

  it('puts the new markup and style into the live page, keeping the node it stands in and what is outside it', () => {
    const listeners: ((ctx: unknown) => void)[] = [];
    const hl = { context: { data: {} }, on: (_: string, f: (ctx: unknown) => void) => listeners.push(f) };
    const root = document.getElementById('hl-widget');
    const scroller = document.createElement('div');
    document.body.appendChild(scroller);
    new Function('hl', patchJS)(hl);
    // The same revision: nothing is touched.
    listeners[0]?.({ data: { content: { rev: 'r1', html: '<i>x</i>', css: 'x{}' } } });
    expect(document.getElementById('n')?.textContent).toBe('10%');
    // The agent's update: the markup and the style change in place.
    listeners[0]?.({ data: { content: { rev: 'r2', html: '<b id="n">60%</b>', css: 'b{color:red}' } } });
    expect(document.getElementById('n')?.textContent).toBe('60%');
    expect(document.getElementById('hl-widget-css')?.textContent).toBe('b{color:red}');
    expect(document.getElementById('hl-widget')).toBe(root);
    expect(scroller.isConnected).toBe(true);
    // The same revision again (the page hands it with every data change): no second write.
    document.getElementById('n')!.textContent = 'kept';
    listeners[0]?.({ data: { content: { rev: 'r2', html: '<b id="n">60%</b>', css: 'b{color:red}' } } });
    expect(document.getElementById('n')?.textContent).toBe('kept');
  });

  it('keeps the last content for the next open and reads the machine again when the widget was installed again', async () => {
    runtimeJson
      .mockResolvedValueOnce({ rev: 'r1', html: '<b>10%</b>', css: '' })
      .mockResolvedValueOnce({ rev: 'r2', html: '<b>60%</b>', css: '' });
    const m = 'm-content' as never;
    const { result, rerender } = renderHook(
      ({ stamp }) => useWidgetContent(m, 'local.widget-w', stamp, true),
      {
        initialProps: { stamp: 'a' },
      },
    );
    expect(result.current.kept).toBeNull();
    await act(async () => {});
    expect(result.current.live?.rev).toBe('r1');
    expect(readKeptContent(m, 'local.widget-w')?.html).toBe('<b>10%</b>');
    rerender({ stamp: 'b' });
    await act(async () => {});
    expect(result.current.live?.rev).toBe('r2');
    // The next chat opened: the card has its last content at the first render, before the machine answers.
    runtimeJson.mockReturnValueOnce(new Promise(() => undefined));
    const next = renderHook(() => useWidgetContent(m, 'local.widget-w', 'b', true));
    expect(next.result.current.kept?.rev).toBe('r2');
    expect(next.result.current.live).toBeNull();
    keepContent(m, 'x', { rev: 'r', html: 'y'.repeat(70_000), css: '' });
    expect(readKeptContent(m, 'x')).toBeNull();
  });
});
