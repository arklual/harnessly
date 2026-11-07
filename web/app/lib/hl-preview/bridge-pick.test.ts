import { describe, expect, it } from 'vitest';
import * as core from './bridge-core';

describe('tk:pick from the parent', () => {
  it('reads a plain pick as before', () => {
    expect(core.parseParentMessage({ type: 'tk:pick', on: true })).toEqual({ type: 'tk:pick', on: true });
    expect(core.parseParentMessage({ type: 'tk:pick', on: 1 })).toBeNull();
  });

  it('reads keep only while picking is turned on', () => {
    expect(core.parseParentMessage({ type: 'tk:pick', on: true, keep: true })).toEqual({
      type: 'tk:pick',
      on: true,
      keep: true,
    });
    expect(core.parseParentMessage({ type: 'tk:pick', on: false, keep: true })).toEqual({
      type: 'tk:pick',
      on: false,
    });
    expect(core.parseParentMessage({ type: 'tk:pick', on: true, keep: 'yes' })).toEqual({
      type: 'tk:pick',
      on: true,
    });
  });
});
