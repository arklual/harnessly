import { describe, expect, it } from 'vitest';
import { cardKindOf } from '@/next/features/chat/link-card';

// One portal serves documents and boards (the runtime's default when only the documents' host is set), the platform
// reads a document at /agents/docs/<slug>.
const config = {
  artifactsUrl: 'https://a.example.org',
  boardsUrl: 'https://a.example.org',
  readerUrl: 'https://ai.example.org/agents/docs/{slug}',
};

// HRN-943: a link is a document or a board by its exact path, never by its host alone.
describe('cardKindOf', () => {
  it.each([
    ['https://a.example.org/a/plan', 'doc'],
    ['https://a.example.org/a/plan/', 'doc'],
    ['https://a.example.org/a/plan?v=2#part', 'doc'],
    ['https://ai.example.org/agents/docs/plan', 'doc'],
    ['https://a.example.org/b/flow', 'board'],
    ['https://a.example.org/b/flow?k=key1', 'board'],
  ])('%s is a %s', (href, kind) => {
    expect(cardKindOf(href, config)).toBe(kind);
  });

  it.each([
    'https://a.example.org/',
    'https://a.example.org/a/plan/v/2',
    'https://a.example.org/b/api',
    'https://ai.example.org/agents/chat/1',
    'https://ai.example.org/agents/docs/',
    'https://stand-1.preview.example.org/a/plan',
    'http://localhost:4400/a/plan',
    'https://a.example.org.evil.io/a/plan',
  ])('%s is no card', (href) => {
    expect(cardKindOf(href, config)).toBeNull();
  });

  it('tells a board apart when the boards have a server of their own', () => {
    const apart = { ...config, boardsUrl: 'https://boards.example.org' };
    expect(cardKindOf('https://boards.example.org/b/flow', apart)).toBe('board');
    expect(cardKindOf('https://a.example.org/b/flow', apart)).toBeNull();
    expect(cardKindOf('https://a.example.org/a/plan', apart)).toBe('doc');
  });
});
