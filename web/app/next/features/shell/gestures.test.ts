// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { ownsSideways } from '@/next/features/shell/gestures';

const mount = (html: string) => {
  document.body.innerHTML = html;
  return document.querySelector('[data-hit]');
};

describe('ownsSideways', () => {
  it('leaves the drawer to a drag over plain chat', () => {
    expect(ownsSideways(mount('<main><p data-hit>text</p></main>'))).toBe(false);
  });

  it('keeps a slider drag, such as the thinking level, off the drawer', () => {
    expect(ownsSideways(mount('<div role="slider"><span data-hit></span></div>'))).toBe(true);
  });

  it('keeps a drag inside a sheet or over its scrim off the drawer', () => {
    expect(ownsSideways(mount('<div role="dialog"><button data-hit></button></div>'))).toBe(true);
    expect(ownsSideways(mount('<div class="hx-sheet-scrim" data-hit></div>'))).toBe(true);
  });
});
