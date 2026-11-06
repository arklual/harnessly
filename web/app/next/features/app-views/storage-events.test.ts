import { describe, expect, it } from 'vitest';
import { parseStorageChange, reaches, type IStorageChange } from '@/next/features/app-views/storage-events';

const change = (over: Partial<IStorageChange> = {}): IStorageChange => ({
  app: 'acme.demo',
  scope: 'chat',
  owner: 'c1',
  key: 'k',
  ...over,
});
const viewer = { app: { id: 'acme.demo', permissions: ['storage'] }, chat: 'c1' as string | null };

describe('storage changes', () => {
  it('reads a change and drops what is not one', () => {
    expect(parseStorageChange('{"app":"a.b","scope":"user","owner":"","key":"k"}')).toEqual({
      app: 'a.b',
      scope: 'user',
      owner: '',
      key: 'k',
    });
    expect(parseStorageChange('nope')).toBeNull();
    expect(parseStorageChange('{"app":"a.b"}')).toBeNull();
  });

  it('goes to the views of the app that changed, of this chat, that hold the storage right', () => {
    expect(reaches(change(), viewer)).toBe(true);
    expect(reaches(change({ scope: 'install', owner: '' }), viewer)).toBe(true);
    expect(reaches(change({ scope: 'space', owner: '' }), viewer)).toBe(true);
  });

  it('does not go to another app, another chat, a view without the right or a view with no chat', () => {
    expect(reaches(change({ app: 'acme.other' }), viewer)).toBe(false);
    expect(reaches(change({ owner: 'c2' }), viewer)).toBe(false);
    expect(reaches(change(), { ...viewer, app: { id: 'acme.demo', permissions: ['notify'] } })).toBe(false);
    expect(reaches(change(), { ...viewer, chat: null })).toBe(false);
    expect(reaches(change({ scope: 'bogus', owner: '' }), viewer)).toBe(false);
  });

  it('an area of a document goes to the views bound to that document only', () => {
    const doc = change({ scope: 'doc', owner: 'd1' });
    expect(reaches(doc, { ...viewer, document: 'd1' })).toBe(true);
    expect(reaches(doc, { ...viewer, document: 'd2' })).toBe(false);
    expect(reaches(doc, viewer)).toBe(false);
  });
});
