import { describe, expect, it } from 'vitest';
import { objectTarget, parseAppTarget } from '@/next/features/app-views/target';

describe('app targets', () => {
  it('reads an app and its view', () => {
    expect(parseAppTarget('acme.thing/home')).toEqual({ app: 'acme.thing', view: 'home' });
    expect(parseAppTarget('object:x')).toBeNull();
  });

  it('reads an object of the app, its home, and nothing else', () => {
    expect(objectTarget('object:plan-x1')).toBe('plan-x1');
    expect(objectTarget('object:')).toBeNull();
    expect(objectTarget('object:a/b')).toBeUndefined();
    expect(objectTarget('app:acme.thing')).toBeUndefined();
  });
});
