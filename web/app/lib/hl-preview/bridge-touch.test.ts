// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import * as core from './bridge-core';

describe('a touched page', () => {
  it('has no hover', () => {
    expect(core.untouchSelector('.btn:hover')).toBe('.btn:hover:not(*)');
    expect(core.untouchSelector('a:hover, a:focus')).toBe('a:hover:not(*), a:focus');
    // Never hovered: what asks for no hover always matches.
    expect(core.untouchSelector('.row:not(:hover) .x')).toBe('.row:not(:hover:not(*)) .x');
    expect(core.untouchSelector('.card:hover-like')).toBe('.card:hover-like');
  });

  it('reads the media of a touch screen', () => {
    expect(core.touchMedia('(hover: hover) and (pointer: fine)')).toBe('(hover: none) and (pointer: coarse)');
    expect(core.touchMedia('(hover:none)')).toBe('(hover: hover)');
    expect(core.touchMedia('(any-pointer: coarse)')).toBe('(any-pointer: fine)');
    expect(core.touchMedia('(min-width: 600px)')).toBe('(min-width: 600px)');
  });

  it('is told by the parent', () => {
    expect(core.parseParentMessage({ type: 'tk:touch', on: true })).toEqual({ type: 'tk:touch', on: true });
    expect(core.parseParentMessage({ type: 'tk:touch', on: 'yes' })).toBeNull();
  });
});
