import { describe, expect, it } from 'vitest';
import { filesTaken, loadStageOf, LOAD_STAGE } from '@/next/features/designly/site-load';

describe('site load stage', () => {
  it('opens first, then loads the resources once the bridge reports the page on its way', () => {
    expect(loadStageOf(null, false)).toBe(LOAD_STAGE.opening);
    expect(loadStageOf({ state: 'loading', resources: 0 }, false)).toBe(LOAD_STAGE.resources);
    expect(loadStageOf({ state: 'interactive', resources: 5 }, false)).toBe(LOAD_STAGE.resources);
  });

  it('says it is longer than usual once the load is slow, whatever was reported', () => {
    expect(loadStageOf(null, true)).toBe(LOAD_STAGE.slow);
    expect(loadStageOf({ state: 'loading', resources: 3 }, true)).toBe(LOAD_STAGE.slow);
  });

  it('counts the files only once there are some', () => {
    expect(filesTaken(null)).toBeNull();
    expect(filesTaken({ state: 'loading', resources: 0 })).toBeNull();
    expect(filesTaken({ state: 'loading', resources: 4 })).toBe(4);
  });
});
