import { describe, expect, it } from 'vitest';
import { objectTarget, parseAppTarget, shareTarget } from '@/next/features/app-views/target';

describe('app targets', () => {
  it('reads an app and its view', () => {
    expect(parseAppTarget('acme.thing/home')).toEqual({ app: 'acme.thing', view: 'home' });
    expect(parseAppTarget('object:x')).toBeNull();
  });

  it('reads an object only after an app view and refuses extra path or query', () => {
    expect(parseAppTarget('acme.thing/board/plan-x1')).toEqual({
      app: 'acme.thing',
      view: 'board',
      object: 'plan-x1',
    });
    for (const target of [
      'acme.thing/board/plan-x1/extra',
      'acme.thing/board/plan-x1?allow_guest=true',
      'acme.thing/board/../secret',
      'acme.thing//plan-x1',
    ])
      expect(parseAppTarget(target), target).toBeNull();
  });

  it('reads an object of the app, its home, and nothing else', () => {
    expect(objectTarget('object:plan-x1')).toBe('plan-x1');
    expect(objectTarget('object:')).toBeNull();
    expect(objectTarget('object:a/b')).toBeUndefined();
    expect(objectTarget('app:acme.thing')).toBeUndefined();
  });
});

describe('share targets (HRN-1519)', () => {
  it('names an object to share and nothing else', () => {
    expect(shareTarget('share:flow-abc')).toBe('flow-abc');
    expect(shareTarget('share:')).toBeNull();
    expect(shareTarget('share:../x')).toBeNull();
    expect(shareTarget('object:flow-abc')).toBeNull();
  });
});
