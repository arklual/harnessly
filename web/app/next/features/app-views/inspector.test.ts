import { describe, expect, it } from 'vitest';
import { resultOf, scopesFor, showsUnwalled } from '@/next/features/app-views/inspector';

describe('resultOf', () => {
  it('says ok, or the code the view got', () => {
    expect(resultOf({ method: 'a', permission: '', ok: true, at: 1 })).toBe('ok');
    expect(resultOf({ method: 'a', permission: 'x', ok: false, code: 'permission_denied', at: 1 })).toBe(
      'permission_denied',
    );
    expect(resultOf({ method: 'a', permission: 'x', ok: false, at: 1 })).toBe('error');
  });
});

describe('scopesFor', () => {
  it('asks for nothing of an app without the storage right', () => {
    expect(scopesFor({ permissions: ['chat.prompt'] }, true)).toEqual([]);
  });

  it('lists the chat scope only where there is a chat, never the document scope', () => {
    expect(scopesFor({ permissions: ['storage'] }, true)).toEqual(['install', 'user', 'chat', 'space']);
    expect(scopesFor({ permissions: ['storage'] }, false)).toEqual(['install', 'user', 'space']);
  });
});

describe('showsUnwalled', () => {
  it('speaks of the wall only where the machine has none and the app works in a chat', () => {
    expect(showsUnwalled({ targets: ['chat', 'app'] }, false)).toBe(true);
    expect(showsUnwalled({ targets: ['chat'] }, true)).toBe(false);
    expect(showsUnwalled({ targets: ['sinopia'] }, false)).toBe(false);
  });
});
