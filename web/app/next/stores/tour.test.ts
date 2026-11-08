import { beforeEach, describe, expect, it } from 'vitest';
import {
  leaveTour,
  startTour,
  stepBack,
  toStep,
  tourFactGroup,
  tourFacts,
  tourState,
} from '@/next/stores/tour';

describe('the tour state', () => {
  beforeEach(() => {
    leaveTour();
    tourFactGroup('lib:route:', []);
  });

  it('knows a step was come back to, and that a step forward is not', () => {
    startTour('site');
    toStep(2);
    expect(tourState()).toMatchObject({ track: 'site', step: 2, back: false });
    stepBack(1);
    expect(tourState()).toMatchObject({ step: 1, back: true });
    toStep(2);
    expect(tourState().back).toBe(false);
  });

  it('counts every arrival, so a step come back to is not the step done before', () => {
    startTour('site');
    const first = tourState().at;
    toStep(1);
    stepBack(0);
    expect(tourState().at).toBeGreaterThan(first + 1);
    expect(tourState().step).toBe(0);
  });

  it('replaces a family of facts at once and leaves the others', () => {
    tourFactGroup('lib:route:', ['lib:route:relay-site:card']);
    tourFactGroup('agent:', []);
    expect([...tourFacts()]).toEqual(['lib:route:relay-site:card']);
    tourFactGroup('lib:route:', ['lib:route:relay-site:gone', 'lib:route:relay-ds:up']);
    expect([...tourFacts()].sort()).toEqual(['lib:route:relay-ds:up', 'lib:route:relay-site:gone']);
    tourFactGroup('lib:route:', []);
    expect(tourFacts().size).toBe(0);
  });
});
