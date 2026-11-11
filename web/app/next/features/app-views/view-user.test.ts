import { describe, expect, it } from 'vitest';
import { viewUser } from '@/next/features/app-views/bridge';

describe('viewUser: hl.context.user (HRN-1152)', () => {
  it('is null while nobody is known', () => {
    expect(viewUser(null, false, false)).toBeNull();
    expect(viewUser({ name: null, email: null }, true, false)).toBeNull();
  });

  it('names the person by name, else by e-mail', () => {
    expect(viewUser({ name: 'Анна', email: 'a@example.org' }, false, false)).toEqual({
      name: 'Анна',
      role: 'member',
    });
    expect(viewUser({ name: '  ', email: 'a@example.org' }, false, false)?.name).toBe('a@example.org');
  });

  it('gives the role in a document: edit, or view when it may only be read', () => {
    expect(viewUser({ name: 'Анна', email: null }, true, false)?.role).toBe('edit');
    expect(viewUser({ name: 'Анна', email: null }, true, true)?.role).toBe('view');
  });
});
