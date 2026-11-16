// @vitest-environment jsdom
/* eslint-disable @typescript-eslint/no-explicit-any -- a frame's window is untyped */
import { describe, expect, it } from 'vitest';
import * as core from './bridge-core';
import { installNativeActs, offSite } from './native-acts';

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

/** A fresh window per case: the acts are installed once per document. */
const freshWindow = () => {
  const frame = document.createElement('iframe');
  document.body.appendChild(frame);
  return frame.contentWindow as any;
};

describe('native pane acts (HRN-1287)', () => {
  it('checks the page is still on the allowed site', () => {
    expect(offSite('https://example.com/a', 'example.com')).toBeNull();
    expect(offSite('https://EXAMPLE.com/a', 'example.com')).toBeNull();
    expect(offSite('https://example.com/a', undefined)).toBeNull();
    expect(offSite('https://other.example.org/', 'example.com')).toContain('other.example.org');
    expect(offSite('not a url', 'example.com')).toContain('unknown');
  });

  it('runs an action and gives its outcome once', async () => {
    const outcome = { ok: true, text: 'clicked', rect: { x: 1, y: 2, width: 3, height: 4 } };
    const fake = { ...core, runAct: () => Promise.resolve(outcome) };
    const win = freshWindow();
    const acts = installNativeActs(win, win.document, fake)!;
    expect(acts).not.toBeNull();
    expect(win.__hlNativeAct).toBe(acts);
    expect(acts.start({ id: 'a1', op: 'click', ref: 'e3' })).toBe(true);
    await settle();
    expect(JSON.parse(acts.take('a1')!)).toEqual({ id: 'a1', ...outcome });
    expect(acts.take('a1')).toBeNull();
  });

  it('refuses a malformed action and one on another site', async () => {
    const win = freshWindow();
    const acts = installNativeActs(win, win.document, {
      ...core,
      runAct: () => Promise.resolve({ ok: true, text: '' }),
    })!;
    expect(acts.start({ id: 'b1', op: 'rm -rf' })).toBe(false);
    expect(acts.start('{')).toBe(false);
    expect(acts.start(null)).toBe(false);
    expect(acts.start({ id: 'b2', op: 'snapshot', host: 'elsewhere.example.org' })).toBe(true);
    const away = JSON.parse(acts.take('b2')!);
    expect(away.ok).toBe(false);
    expect(away.text).toContain('elsewhere.example.org');
  });

  it('answers a failing action plainly and installs only once', async () => {
    const win = freshWindow();
    const acts = installNativeActs(win, win.document, {
      ...core,
      runAct: () => Promise.reject(new Error('boom')),
    })!;
    expect(acts.start(JSON.stringify({ id: 'c1', op: 'snapshot' }))).toBe(true);
    await settle();
    expect(JSON.parse(acts.take('c1')!)).toMatchObject({ ok: false, text: expect.stringContaining('boom') });
    expect(installNativeActs(win, win.document, core)).toBeNull();
  });
});
